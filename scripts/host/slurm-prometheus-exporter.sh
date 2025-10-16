#!/bin/bash

set -eo pipefail

# Go configuration
GO_INSTALL_DIR="/usr/local"
MIN_GO_VERSION="1.20.0"

# Default configuration
DEFAULT_PORT="9092"
DEFAULT_LOG_LEVEL="info"
DEFAULT_METRICS_PATH="/metrics"
DEFAULT_COLLECT_DIAGS="false"
DEFAULT_COLLECT_LICENSES="false"
DEFAULT_COLLECT_LIMITS="false"

# Configuration variables (can be overridden by command line)
PORT="${DEFAULT_PORT}"
LOG_LEVEL="${DEFAULT_LOG_LEVEL}"
METRICS_PATH="${DEFAULT_METRICS_PATH}"
COLLECT_DIAGS="${DEFAULT_COLLECT_DIAGS}"
COLLECT_LICENSES="${DEFAULT_COLLECT_LICENSES}"
COLLECT_LIMITS="${DEFAULT_COLLECT_LIMITS}"

# Vector.dev configuration
ENABLE_VECTOR="false"
VECTOR_INSTALL_DIR="$HOME/.vector"

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Install and configure prometheus-slurm-exporter with systemd integration.

OPTIONS:
    -p, --port PORT          Listen port (default: 9092)
    -l, --log-level LEVEL    Log level: info, debug, error, warning (default: info)
    -m, --metrics-path PATH  Metrics endpoint path (default: /metrics)
    --collect-diags          Enable SLURM diagnostics collection
    --collect-licenses       Enable SLURM license collection  
    --collect-limits         Enable SLURM account limits collection
    --enable-vector          Install and configure Vector.dev for metrics shipping
    --uninstall             Completely remove installation
    -h, --help              Show this help message

EXAMPLES:
    $0                                    # Install with default settings
    $0 --port=9093 --log-level=debug     # Custom port and debug logging
    $0 --collect-diags --collect-licenses # Enable additional collectors
    $0 --enable-vector                   # Install with Vector.dev integration
    $0 --uninstall                       # Remove everything

EOF
}

get_latest_go_version() {
    local latest_version
    
    if command -v curl &>/dev/null; then
        latest_version=$(curl -s "https://go.dev/VERSION?m=text" 2>/dev/null | head -n1 | sed 's/go//')
    elif command -v wget &>/dev/null; then
        latest_version=$(wget -qO- "https://go.dev/VERSION?m=text" 2>/dev/null | head -n1 | sed 's/go//')
    fi
    
    if [ -z "$latest_version" ] || [[ ! "$latest_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        latest_version="1.23.2"
    fi
    
    echo "$latest_version"
}

# Define basic packages (no Go packages)
YUM_PACKAGES=(
    wget git make gcc
)

APT_PACKAGES=(
    wget git make gcc
)

install_apt_packages() {
    if [ "$EUID" -ne 0 ]; then
        echo "Package installation requires root privileges. Please run with sudo." >&2
        exit 1
    fi
    apt-get update
    apt-get install -y "${APT_PACKAGES[@]}" || echo "Failed to install APT packages" >&2
}

install_yum_packages() {
    if [ "$EUID" -ne 0 ]; then
        echo "Package installation requires root privileges. Please run with sudo." >&2
        exit 1
    fi
    yum install -y --skip-broken "${YUM_PACKAGES[@]}" || echo "Failed to install YUM packages" >&2
}

version_compare() {
    local ver1=$1
    local ver2=$2
    
    if [[ "$ver1" == "$ver2" ]]; then
        return 0
    fi
    
    local IFS=.
    local i ver1_arr=($ver1) ver2_arr=($ver2)
    
    for ((i=${#ver1_arr[@]}; i<${#ver2_arr[@]}; i++)); do
        ver1_arr[i]=0
    done
    
    for ((i=0; i<${#ver1_arr[@]}; i++)); do
        if [[ -z ${ver2_arr[i]} ]]; then
            ver2_arr[i]=0
        fi
        if ((10#${ver1_arr[i]} > 10#${ver2_arr[i]})); then
            return 0
        fi
        if ((10#${ver1_arr[i]} < 10#${ver2_arr[i]})); then
            return 1
        fi
    done
    return 0
}

check_go_version() {
    if [ -f /etc/profile.d/go.sh ]; then
        source /etc/profile.d/go.sh
    fi
    
    local go_binary=""
    if command -v go &>/dev/null; then
        go_binary=$(command -v go)
    elif [ -f "/usr/local/go/bin/go" ]; then
        go_binary="/usr/local/go/bin/go"
        export PATH="/usr/local/go/bin:$PATH"
    fi
    
    if [ -n "$go_binary" ]; then
        local current_version=$($go_binary version | awk '{print $3}' | sed 's/go//')
        echo "Found Go version: $current_version"
        
        if version_compare "$current_version" "$MIN_GO_VERSION"; then
            echo "Go version $current_version is acceptable (>= $MIN_GO_VERSION)"
            return 0
        else
            echo "ERROR: Go version $current_version is less than required version $MIN_GO_VERSION" >&2
            echo "Please upgrade your Go installation to version $MIN_GO_VERSION or higher" >&2
            exit 1
        fi
    else
        echo "Go is not installed"
        return 1
    fi
}

install_go_manually() {
    if [ "$EUID" -ne 0 ]; then
        echo "Go installation requires root privileges. Please run with sudo." >&2
        exit 1
    fi
    
    echo "Fetching latest Go version..."
    local GO_VERSION=$(get_latest_go_version)
    echo "Latest Go version found: ${GO_VERSION}"
    echo "Installing Go ${GO_VERSION}..."
    
    local arch
    case $(uname -m) in
        x86_64) arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
    esac
    
    local go_tarball="go${GO_VERSION}.linux-${arch}.tar.gz"
    local download_url="https://golang.org/dl/${go_tarball}"
    
    echo "Downloading Go from ${download_url}..."
    wget -O "/tmp/${go_tarball}" "${download_url}"
    
    if [ -d "${GO_INSTALL_DIR}/go" ]; then
        echo "Removing existing Go installation..."
        rm -rf "${GO_INSTALL_DIR}/go"
    fi
    
    echo "Extracting Go to ${GO_INSTALL_DIR}..."
    tar -C "${GO_INSTALL_DIR}" -xzf "/tmp/${go_tarball}"
    
    rm -f "/tmp/${go_tarball}"
    
    cat > /etc/profile.d/go.sh << 'EOF'
export GOROOT=/usr/local/go
export GOPATH=$HOME/go
export PATH=$GOROOT/bin:$GOPATH/bin:$PATH
EOF
    
    chmod +x /etc/profile.d/go.sh
    source /etc/profile.d/go.sh
    
    echo "Go ${GO_VERSION} installed successfully"
}

install_prometheus_slurm_exporter() {
    echo "Installing prometheus-slurm-exporter..."
    
    if [ -f /etc/profile.d/go.sh ]; then
        source /etc/profile.d/go.sh
    fi
    
    if ! command -v go &>/dev/null; then
        echo "ERROR: Go is not available in PATH" >&2
        exit 1
    fi
    
    echo "Installing from github.com/rivosinc/prometheus-slurm-exporter..."
    go install github.com/rivosinc/prometheus-slurm-exporter@latest
    
    if command -v prometheus-slurm-exporter &>/dev/null; then
        echo "✓ prometheus-slurm-exporter installed successfully"
        echo "Installation path: $(command -v prometheus-slurm-exporter)"
        echo ""
        echo "Testing help output:"
        prometheus-slurm-exporter -h
    else
        echo "ERROR: prometheus-slurm-exporter installation failed or not found in PATH" >&2
        echo "Make sure \$GOPATH/bin is in your PATH" >&2
        exit 1
    fi
}

test_prometheus_slurm_exporter() {
    echo "Testing prometheus-slurm-exporter functionality..."
    
    local listen_addr=":${PORT}"
    local metrics_path="${METRICS_PATH}"
    local test_url="http://localhost${listen_addr}${metrics_path}"
    local max_attempts=10
    local attempt=1
    
    echo "Testing endpoint: $test_url"
    
    while [ $attempt -le $max_attempts ]; do
        echo "Attempt $attempt/$max_attempts - Testing connectivity..."
        
        if command -v curl &>/dev/null; then
            if curl -s -f "$test_url" >/dev/null 2>&1; then
                echo "✓ Exporter is responding!"
                break
            fi
        elif command -v wget &>/dev/null; then
            if wget -q -O /dev/null "$test_url" 2>/dev/null; then
                echo "✓ Exporter is responding!"
                break
            fi
        else
            echo "ERROR: Neither curl nor wget is available for testing" >&2
            return 1
        fi
        
        if [ $attempt -eq $max_attempts ]; then
            echo "ERROR: Exporter is not responding after $max_attempts attempts" >&2
            echo "Make sure the exporter is running with: prometheus-slurm-exporter -web.listen-address=\":9092\" &" >&2
            return 1
        fi
        
        sleep 2
        ((attempt++))
    done
    
    echo "Checking for SLURM metrics..."
    local metrics_output
    
    if command -v curl &>/dev/null; then
        metrics_output=$(curl -s "$test_url" 2>/dev/null)
    elif command -v wget &>/dev/null; then
        metrics_output=$(wget -q -O - "$test_url" 2>/dev/null)
    fi
    
    local slurm_metrics=(
        "slurm_cpus_total"
        "slurm_node_count_per_state"
        "slurm_partition_total_cpus"
        "slurm_job_scrape_duration"
        "slurm_node_scrape_duration"
    )
    
    local found_metrics=0
    for metric in "${slurm_metrics[@]}"; do
        if echo "$metrics_output" | grep -q "$metric"; then
            echo "✓ Found metric: $metric"
            ((found_metrics++))
        else
            echo "✗ Missing metric: $metric"
        fi
    done
    
    if [ $found_metrics -eq ${#slurm_metrics[@]} ]; then
        echo "✓ All expected SLURM metrics are present ($found_metrics/${#slurm_metrics[@]})"
        
        echo ""
        echo "Sample SLURM metrics:"
        echo "$metrics_output" | grep "^slurm_" | head -10
        
        echo ""
        echo "✓ prometheus-slurm-exporter is working correctly!"
        return 0
    else
        echo "✗ Only found $found_metrics/${#slurm_metrics[@]} expected metrics"
        return 1
    fi
}

create_systemd_service() {
    echo "Creating systemd service for prometheus-slurm-exporter..."
    
    if [ "$EUID" -ne 0 ]; then
        echo "Creating systemd service requires root privileges. Please run with sudo." >&2
        return 1
    fi
    
    local service_name="prometheus-slurm-exporter"
    local service_file="/etc/systemd/system/${service_name}.service"
    
    if ! command -v prometheus-slurm-exporter &>/dev/null; then
        echo "ERROR: prometheus-slurm-exporter command not found in PATH" >&2
        echo "Make sure the exporter is installed and available globally" >&2
        echo "You may need to add the Go bin directory to your PATH" >&2
        return 1
    fi
    
    local exporter_binary=$(command -v prometheus-slurm-exporter)
    local system_binary_path="/usr/local/bin/prometheus-slurm-exporter"
    
    echo "Found exporter at: $exporter_binary"
    
    # If exporter is in a user directory, copy it to a system location
    if [[ "$exporter_binary" == /home/* ]] || [[ "$exporter_binary" == /root/* ]]; then
        echo "Copying exporter from user directory to system location..."
        cp "$exporter_binary" "$system_binary_path"
        chmod 755 "$system_binary_path"
        exporter_binary="$system_binary_path"
        echo "Exporter copied to: $system_binary_path"
    fi
    
    if ! id "prometheus" &>/dev/null; then
        echo "Creating prometheus user..."
        useradd -r -s /bin/false -d /var/lib/prometheus prometheus
        mkdir -p /var/lib/prometheus
        chown prometheus:prometheus /var/lib/prometheus
        
        # Add prometheus user to necessary groups for SLURM access
        if getent group slurm >/dev/null 2>&1; then
            usermod -a -G slurm prometheus
            echo "Added prometheus user to slurm group"
        fi
    fi
    
    # Ensure the binary has proper permissions
    chmod 755 "$exporter_binary"
    
    cat > "$service_file" << EOF
[Unit]
Description=Prometheus SLURM Exporter
Documentation=https://github.com/rivosinc/prometheus-slurm-exporter
After=network.target slurm.service
Wants=network.target

[Service]
Type=simple
User=prometheus
Group=prometheus
ExecStart=$exporter_binary \\
    -web.listen-address=":9092" \\
    -web.telemetry-path="/metrics" \\
    -web.log-level="info" \\
    -slurm.collect-diags=false \\
    -slurm.collect-licenses=false \\
    -slurm.collect-limits=false \\
    -slurm.cli-fallback=true \\
    -slurm.poll-limit=10.0

# Security settings
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/prometheus
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true

# Restart policy
Restart=always
RestartSec=5
TimeoutStopSec=20

[Install]
WantedBy=multi-user.target
EOF

    chmod 644 "$service_file"
    
    echo "✓ Systemd service file created: $service_file"
    echo "Reloading systemd daemon..."
    systemctl daemon-reload
    
    return 0
}

manage_systemd_service() {
    local action="$1"
    local service_name="prometheus-slurm-exporter"
    
    if [ "$EUID" -ne 0 ]; then
        echo "Service management requires root privileges. Please run with sudo." >&2
        return 1
    fi
    
    case "$action" in
        "start")
            echo "Starting $service_name service..."
            systemctl start "$service_name"
            ;;
        "stop")
            echo "Stopping $service_name service..."
            systemctl stop "$service_name"
            ;;
        "restart")
            echo "Restarting $service_name service..."
            systemctl restart "$service_name"
            ;;
        "enable")
            echo "Enabling $service_name to start on boot..."
            systemctl enable "$service_name"
            ;;
        "disable")
            echo "Disabling $service_name from starting on boot..."
            systemctl disable "$service_name"
            ;;
        "status")
            systemctl status "$service_name" --no-pager
            ;;
        *)
            echo "Unknown action: $action" >&2
            echo "Available actions: start, stop, restart, enable, disable, status" >&2
            return 1
            ;;
    esac
}

check_systemd_service() {
    local service_name="prometheus-slurm-exporter"
    
    if systemctl is-active --quiet "$service_name"; then
        echo "✓ Service $service_name is running"
        return 0
    else
        echo "✗ Service $service_name is not running"
        return 1
    fi
}

install_vector() {
    echo "Installing Vector.dev..."
    
    if command -v vector &>/dev/null; then
        echo "✓ Vector is already installed"
        vector --version
        return 0
    fi
    
    if [ -f "$VECTOR_INSTALL_DIR/bin/vector" ]; then
        echo "✓ Vector is already installed at $VECTOR_INSTALL_DIR"
        "$VECTOR_INSTALL_DIR/bin/vector" --version
        
        if ! command -v vector &>/dev/null; then
            export PATH="$VECTOR_INSTALL_DIR/bin:$PATH"
            echo "Added Vector to PATH for current session"
        fi
        return 0
    fi
    
    echo "Downloading and installing Vector.dev..."
    
    if curl --proto '=https' --tlsv1.2 -sSfL https://sh.vector.dev | bash -s -- -y; then
        echo "✓ Vector installed successfully"
        
        if [ -f ~/.profile ]; then
            source ~/.profile
        fi
        
        if [ -f "$VECTOR_INSTALL_DIR/bin/vector" ]; then
            export PATH="$VECTOR_INSTALL_DIR/bin:$PATH"
        fi
        
        if command -v vector &>/dev/null; then
            echo "Installation path: $(command -v vector)"
            vector --version
            return 0
        elif [ -f "$VECTOR_INSTALL_DIR/bin/vector" ]; then
            echo "Installation path: $VECTOR_INSTALL_DIR/bin/vector"
            "$VECTOR_INSTALL_DIR/bin/vector" --version
            return 0
        else
            echo "ERROR: Vector installation completed but binary not found" >&2
            return 1
        fi
    else
        echo "ERROR: Vector installation failed" >&2
        return 1
    fi
}



uninstall() {
    echo "Uninstalling prometheus-slurm-exporter..."
    
    if [ "$EUID" -ne 0 ]; then
        echo "Uninstall requires root privileges. Please run with sudo." >&2
        exit 1
    fi
    
    if systemctl is-active --quiet prometheus-slurm-exporter 2>/dev/null; then
        echo "Stopping prometheus-slurm-exporter service..."
        systemctl stop prometheus-slurm-exporter
    fi
    
    if systemctl is-enabled --quiet prometheus-slurm-exporter 2>/dev/null; then
        echo "Disabling prometheus-slurm-exporter service..."
        systemctl disable prometheus-slurm-exporter
    fi
    
    if [ -f "/etc/systemd/system/prometheus-slurm-exporter.service" ]; then
        echo "Removing systemd service file..."
        rm -f /etc/systemd/system/prometheus-slurm-exporter.service
        systemctl daemon-reload
    fi
    
    if [ -f "/usr/local/bin/prometheus-slurm-exporter" ]; then
        echo "Removing system binary..."
        rm -f /usr/local/bin/prometheus-slurm-exporter
    fi
    
    if id "prometheus" &>/dev/null; then
        read -p "Remove prometheus user? [y/N]: " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo "Removing prometheus user..."
            userdel -r prometheus 2>/dev/null || true
        else
            echo "Keeping prometheus user (may be used by other services)"
        fi
    fi
    
    local user_paths=(
        "/home/ubuntu/go/bin/prometheus-slurm-exporter"
        "/home/$USER/go/bin/prometheus-slurm-exporter"
        "/root/go/bin/prometheus-slurm-exporter"
        "$HOME/go/bin/prometheus-slurm-exporter"
    )
    
    for path in "${user_paths[@]}"; do
        if [ -f "$path" ]; then
            echo "Removing user binary: $path"
            rm -f "$path"
        fi
    done
    
    if pgrep -f prometheus-slurm-exporter >/dev/null 2>&1; then
        echo "Stopping any running prometheus-slurm-exporter processes..."
        pkill -f prometheus-slurm-exporter || true
    fi
    
    read -p "Remove Go installation? [y/N]: " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Removing Go installation..."
        rm -rf /usr/local/go
        rm -f /etc/profile.d/go.sh
        echo "Go removed. You may need to restart your shell or logout/login."
    else
        echo "Keeping Go installation"
    fi
    
    echo "✅ Uninstall completed!"
    exit 0
}

parse_arguments() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -p|--port)
                PORT="$2"
                shift 2
                ;;
            --port=*)
                PORT="${1#*=}"
                shift
                ;;
            -l|--log-level)
                LOG_LEVEL="$2"
                shift 2
                ;;
            --log-level=*)
                LOG_LEVEL="${1#*=}"
                shift
                ;;
            -m|--metrics-path)
                METRICS_PATH="$2"
                shift 2
                ;;
            --metrics-path=*)
                METRICS_PATH="${1#*=}"
                shift
                ;;
            --collect-diags)
                COLLECT_DIAGS="true"
                shift
                ;;
            --collect-licenses)
                COLLECT_LICENSES="true"
                shift
                ;;
            --collect-limits)
                COLLECT_LIMITS="true"
                shift
                ;;
            --enable-vector)
                ENABLE_VECTOR="true"
                shift
                ;;
            --uninstall)
                uninstall
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1" >&2
                usage
                exit 1
                ;;
        esac
    done
    
    if ! [[ "$PORT" =~ ^[0-9]+$ ]] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
        echo "Error: Invalid port number: $PORT" >&2
        exit 1
    fi
    
    case "$LOG_LEVEL" in
        info|debug|error|warning) ;;
        *) echo "Error: Invalid log level: $LOG_LEVEL. Must be one of: info, debug, error, warning" >&2; exit 1 ;;
    esac
    
    if [[ ! "$METRICS_PATH" =~ ^/ ]]; then
        echo "Error: Metrics path must start with /: $METRICS_PATH" >&2
        exit 1
    fi
}

main() {
    echo "Checking system setup..."
    
    local missing_packages=()
    local basic_commands=("wget" "git" "make" "gcc")
    
    for cmd in "${basic_commands[@]}"; do
        if ! command -v "$cmd" &>/dev/null; then
            missing_packages+=("$cmd")
        fi
    done
    
    if [ ${#missing_packages[@]} -gt 0 ]; then
        echo "Missing basic packages: ${missing_packages[*]}"
        echo "Installing basic packages..."
        
        if [ "$EUID" -ne 0 ]; then
            echo "Package installation requires root privileges. Please run with sudo." >&2
            exit 1
        fi
        
        if [ -f /etc/os-release ]; then
            . /etc/os-release
            case "$ID" in
            debian | ubuntu | pop)
                install_apt_packages
                ;;
            rhel | centos | fedora | amzn)
                install_yum_packages
                ;;
            *)
                echo "Unknown distribution: $ID" >&2
                exit 1
                ;;
            esac
        elif [ -f /etc/debian_version ]; then
            install_apt_packages
        elif [ -f /etc/redhat-release ]; then
            install_yum_packages
        else
            echo "Unknown distribution" >&2
            exit 1
        fi
    else
        echo "✓ Basic packages are already installed"
    fi
    
    echo "Checking Go installation..."
    if check_go_version; then
        echo "✓ Go is already installed and meets requirements"
    else
        echo "Installing Go..."
        if [ "$EUID" -ne 0 ]; then
            echo "Go installation requires root privileges. Please run with sudo." >&2
            exit 1
        fi
        install_go_manually
    fi
    
    echo "Go environment setup completed!"
    echo "Go version: $(go version)"
    
    echo "Checking prometheus-slurm-exporter installation..."
    
    local exporter_found=false
    local exporter_path=""
    
    if command -v prometheus-slurm-exporter &>/dev/null; then
        exporter_found=true
        exporter_path=$(command -v prometheus-slurm-exporter)
    else
        local possible_paths=(
            "/home/ubuntu/go/bin/prometheus-slurm-exporter"
            "/home/$USER/go/bin/prometheus-slurm-exporter"
            "/root/go/bin/prometheus-slurm-exporter"
            "$HOME/go/bin/prometheus-slurm-exporter"
        )
        
        for path in "${possible_paths[@]}"; do
            if [ -f "$path" ] && [ -x "$path" ]; then
                exporter_found=true
                exporter_path="$path"
                export PATH="$(dirname "$path"):$PATH"
                echo "Found exporter at $path, added to PATH"
                break
            fi
        done
    fi
    
    if [ "$exporter_found" = true ]; then
        echo "✓ prometheus-slurm-exporter is already installed"
        echo "Installation path: $exporter_path"
        echo "Version info:"
        "$exporter_path" -h | head -1 || echo "Available at: $exporter_path"
    else
        echo "Installing prometheus-slurm-exporter..."
        install_prometheus_slurm_exporter
    fi
    
    echo ""
    echo "✓ Setup completed! All components are ready."
    
    echo ""
    echo "Checking systemd service..."
    local service_exists=false
    if [ -f "/etc/systemd/system/prometheus-slurm-exporter.service" ]; then
        service_exists=true
        echo "✓ Systemd service file exists"
        
        if systemctl is-active --quiet prometheus-slurm-exporter; then
            echo "✓ Service is running"
            echo ""
            echo "✓ Found running service! Running full test..."
            test_prometheus_slurm_exporter || echo "⚠ Test completed with warnings"
        elif systemctl is-enabled --quiet prometheus-slurm-exporter; then
            echo "ℹ Service is enabled but not running"
            if [ "$EUID" -eq 0 ]; then
                echo "Starting the service..."
                manage_systemd_service start
                sleep 2
                if check_systemd_service; then
                    echo ""
                    test_prometheus_slurm_exporter || echo "⚠ Test completed with warnings"
                fi
            else
                echo "Run 'sudo systemctl start prometheus-slurm-exporter' to start it"
            fi
        else
            echo "ℹ Service exists but is not enabled"
            if [ "$EUID" -eq 0 ]; then
                echo "Enabling and starting the service..."
                manage_systemd_service enable
                manage_systemd_service start
                sleep 2
                if check_systemd_service; then
                    echo ""
                    test_prometheus_slurm_exporter || echo "⚠ Test completed with warnings"
                fi
            else
                echo "Run 'sudo systemctl enable --now prometheus-slurm-exporter' to enable and start it"
            fi
        fi
    else
        echo "ℹ No systemd service found"
        if [ "$EUID" -eq 0 ]; then
            echo "Creating systemd service..."
            if create_systemd_service; then
                echo "Enabling and starting the service..."
                manage_systemd_service enable
                manage_systemd_service start
                sleep 2
                if check_systemd_service; then
                    echo ""
                    test_prometheus_slurm_exporter || echo "⚠ Test completed with warnings"
                else
                    echo "Service creation completed, but service is not running properly"
                fi
            fi
        else
            echo "Run with sudo to create systemd service, or start manually:"
            if [ -n "$exporter_path" ]; then
                echo "  $exporter_path -web.listen-address=\":9092\" &"
            else
                echo "  prometheus-slurm-exporter -web.listen-address=\":9092\" &"
            fi
        fi
    fi
    
    if [ "$service_exists" = false ] && [ "$EUID" -ne 0 ]; then
        echo ""
        echo "Checking for manually started exporter..."
        if curl -s -f "http://localhost:${PORT}${METRICS_PATH}" >/dev/null 2>&1 || wget -q -O /dev/null "http://localhost:${PORT}${METRICS_PATH}" 2>/dev/null; then
            echo "✓ Found running exporter! Running full test..."
            echo ""
            test_prometheus_slurm_exporter || echo "⚠ Test completed with warnings"
        fi
    fi
}

# Parse command line arguments
parse_arguments "$@"

# Show configuration
echo "📋 Configuration:"
echo "   Port: ${PORT}"
echo "   Log Level: ${LOG_LEVEL}"
echo "   Metrics Path: ${METRICS_PATH}"
echo "   Collect Diags: ${COLLECT_DIAGS}"
echo "   Collect Licenses: ${COLLECT_LICENSES}"
echo "   Collect Limits: ${COLLECT_LIMITS}"
echo "   Enable Vector.dev: ${ENABLE_VECTOR}"
echo ""

# Run main installation
main

if [ "$ENABLE_VECTOR" = "true" ]; then
    echo ""
    echo "============================================"
    echo "Installing Vector.dev..."
    echo "============================================"
    
    if install_vector; then
        echo ""
        echo "✅ Vector.dev installation completed!"
        echo ""
        echo "Vector is installed at:"
        if command -v vector &>/dev/null; then
            echo "  $(command -v vector)"
        else
            echo "  $VECTOR_INSTALL_DIR/bin/vector"
        fi
        echo ""
        echo "Next steps:"
        echo "1. Create Vector config: /etc/vector/vector.yaml (or custom location)"
        echo "2. Test Vector: vector --config <config-file>"
        echo "3. Run Vector: vector --config <config-file>"
        echo ""
        echo "Note: Vector is installed in user directory ($VECTOR_INSTALL_DIR)"
        echo "      You may need to source ~/.profile in new shells"
    else
        echo ""
        echo "❌ Vector.dev installation failed"
        echo "Please check the error messages above"
        exit 1
    fi
fi
