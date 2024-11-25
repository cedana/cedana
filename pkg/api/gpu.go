package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	gpugrpc "buf.build/gen/go/cedana/gpu/grpc/go/cedanagpu/cedanagpugrpc"
	gpu "buf.build/gen/go/cedana/gpu/protocolbuffers/go/cedanagpu"
	task "buf.build/gen/go/cedana/task/protocolbuffers/go"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	GPU_CONTROLLER_WAIT_TIMEOUT       = 30 * time.Second
	GPU_CONTROLLER_DEFAULT_HOST       = "localhost" // and port is dynamic
	GPU_CONTROLLER_LOG_PATH_FORMATTER = "/tmp/cedana-gpu-controller-%s.log"
)

// Map of GPU controllers, indexed by JID
var gpuControllers = make(map[string]*GPUController)

type GPUController struct {
	Cmd    *exec.Cmd
	Client gpugrpc.CedanaGPUClient
	Conn   *grpc.ClientConn
	Output *bytes.Buffer
}

func (s *service) GPUHealthCheck(
	ctx context.Context,
	req *task.DetailedHealthCheckRequest,
	resp *task.DetailedHealthCheckResponse,
) error {
	gpuControllerPath := viper.GetString("gpu_controller_path")
	if gpuControllerPath == "" {
		gpuControllerPath = utils.GpuControllerBinaryPath
	}

	gpuSharedLibPath := viper.GetString("gpu_shared_lib_path")
	if gpuSharedLibPath == "" {
		gpuSharedLibPath = utils.GpuSharedLibPath
	}

	if _, err := os.Stat(gpuControllerPath); os.IsNotExist(err) {
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("gpu controller binary not found at %s", gpuControllerPath))
	}

	if _, err := os.Stat(gpuSharedLibPath); os.IsNotExist(err) {
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("gpu shared lib not found at %s", gpuSharedLibPath))
	}

	if len(resp.UnhealthyReasons) != 0 {
		return nil
	}

	gpuOutBuf := &bytes.Buffer{}
	err := s.StartGPUController(ctx, req.UID, req.GID, req.Groups, "healthcheck")
	if err != nil {
		log.Error().Err(err).Str("stdout/stderr", gpuOutBuf.String()).Msg("could not start gpu controller")
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("could not start gpu controller: %v", err))
		return nil
	}
	defer func() {
		err = s.StopGPUController("healthcheck")
		if err != nil {
			log.Error().Err(err).Msg("could not stop gpu controller")
		}
		err = s.WaitGPUController("healthcheck")
		if err != nil {
			log.Error().Err(err).Msg("could not wait for gpu controller")
		}
	}()

	gpuController := s.GetGPUController("healthcheck")
	if gpuController == nil {
		return fmt.Errorf("gpu controller not found")
	}

	args := gpu.HealthCheckRequest{}
	gpuResp, err := gpuController.Client.HealthCheck(ctx, &args)
	if err != nil {
		return err
	}

	if !gpuResp.Success {
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, "gpu health check did not return success")
	}

	resp.HealthCheckStats.GPUHealthCheck = gpuResp

	return nil
}

func (s *service) StartGPUController(ctx context.Context, uid, gid int32, groups []int32, jid string) error {
	log.Debug().Int32("UID", uid).Int32("GID", gid).Ints32("Groups", groups).Str("JID", jid).Msgf("starting gpu controller")
	var gpuCmd *exec.Cmd
	controllerPath := viper.GetString("gpu_controller_path")
	if controllerPath == "" {
		controllerPath = utils.GpuControllerBinaryPath
	}
	if _, err := os.Stat(controllerPath); os.IsNotExist(err) {
		log.Error().Err(err).Send()
		return fmt.Errorf("no gpu controller at %s", controllerPath)
	}

	if viper.GetBool("gpu_debugging_enabled") {
		controllerPath = strings.Join([]string{
			"compute-sanitizer",
			"--log-file /tmp/cedana-sanitizer.log",
			"--print-level info",
			"--leak-check=full",
			controllerPath,
		},
			" ")
	}

	// get free port for gpu controller
	port, err := utils.GetFreePort()
	if err != nil {
		return fmt.Errorf("could not get free port for gpu controller: %v", err)
	}

	gpuCmd = exec.CommandContext(s.serverCtx, controllerPath, jid, "--port", strconv.Itoa(port))
	groupsUint32 := make([]uint32, len(groups))
	for i, v := range groups {
		groupsUint32[i] = uint32(v)
	}
	gpuCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid:    uint32(uid),
			Gid:    uint32(gid),
			Groups: groupsUint32,
		},
	}

	outBuf := &bytes.Buffer{}
	gpuOut := io.MultiWriter(outBuf)
	gpuCmd.Stderr = gpuOut
	gpuCmd.Stdout = gpuOut

	// to ensure GPU controller is killed gracefully even if server dies
	gpuCmd.Cancel = func() error { return gpuCmd.Process.Signal(syscall.SIGTERM) }

	gpuCmd.Env = append(
		os.Environ(),
		"CEDANA_AUTH_TOKEN="+viper.GetString("connection.cedana_auth_token"),
		"CEDANA_URL="+viper.GetString("connection.cedana_url"),
	)

	err = gpuCmd.Start()
	if err != nil {
		log.Error().Err(err).Str("stdout/stderr", outBuf.String()).Msg("failed to start GPU controller")
		return fmt.Errorf("could not start gpu controller %v", err)
	}
	log.Debug().Int("PID", gpuCmd.Process.Pid).Msgf("GPU controller starting...")

	// poll gpu controller to ensure it is running
	var opts []grpc.DialOption
	var gpuConn *grpc.ClientConn
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	gpuConn, err = grpc.NewClient(fmt.Sprintf("%s:%d", GPU_CONTROLLER_DEFAULT_HOST, port), opts...)
	if err != nil {
		log.Error().Err(err).Str("stdout/stderr", outBuf.String()).Msg("failed to start GPU controller")
		return fmt.Errorf("could not connect to gpu controller %v", err)
	}

	gpuServiceConn := gpugrpc.NewCedanaGPUClient(gpuConn)

	args := gpu.StartupPollRequest{}
	waitCtx, _ := context.WithTimeout(ctx, GPU_CONTROLLER_WAIT_TIMEOUT)
	resp, err := gpuServiceConn.StartupPoll(waitCtx, &args, grpc.WaitForReady(true))
	if err != nil || !resp.Success {
		gpuCmd.Process.Signal(syscall.SIGTERM)
		gpuConn.Close()
		log.Error().Err(err).Str("stdout/stderr", outBuf.String()).Msg("failed to start GPU controller")
		return fmt.Errorf("gpu controller did not start: %v", err)
	}

	log.Debug().Int("PID", gpuCmd.Process.Pid).Str("Log", fmt.Sprintf(GPU_CONTROLLER_LOG_PATH_FORMATTER, jid)).Msgf("GPU controller started")

	gpuControllers[jid] = &GPUController{
		Cmd:    gpuCmd,
		Client: gpuServiceConn,
		Conn:   gpuConn,
		Output: outBuf,
	}

	return nil
}

func (s *service) StopGPUController(jid string) error {
	controller, ok := gpuControllers[jid]
	if !ok {
		return fmt.Errorf("no gpu controller found for jid %s", jid)
	}

	if controller.Cmd != nil {
		controller.Cmd.Process.Signal(syscall.SIGTERM) // SIGTERM for graceful exit
	}

	if controller.Conn != nil {
		controller.Conn.Close()
	}

	return nil
}

func (s *service) WaitGPUController(jid string) error {
	gpuController, ok := gpuControllers[jid]
	if !ok {
		return fmt.Errorf("no gpu controller found for jid %s", jid)
	}
	cmd := gpuController.Cmd
	if cmd != nil {
		err := cmd.Wait()
		if err != nil {
			log.Debug().Err(err).Msg("GPU controller Wait()")
		}
		log.Info().Int("PID", cmd.Process.Pid).
			Int("status", cmd.ProcessState.ExitCode()).
			Str("stdout/stderr", gpuController.Output.String()).
			Msg("GPU controller exited")
	}

	s.StopGPUController(jid)
	delete(gpuControllers, jid)

	return nil
}

func (s *service) GetGPUController(jid string) *GPUController {
	controller, ok := gpuControllers[jid]
	if !ok {
		return nil
	}

	return controller
}

func DownloadGPUBinaries(ctx context.Context) error {
	var wg sync.WaitGroup
	var err error
	wg.Add(2)
	go func() {
		defer wg.Done()
		gpuControllerPath := utils.GpuControllerBinaryPath
		if s := viper.GetString("gpu_controller_path"); s != "" {
			gpuControllerPath = s
		}
		log.Info().Str("gpu_controller_path", gpuControllerPath).Msg("Ensuring GPU Controller exists.")
		err = pullGPUBinary(ctx, utils.GpuControllerBinaryName, gpuControllerPath)
		if err != nil {
			log.Error().Err(err).Msg("could not pull gpu controller")
			return
		}
	}()

	go func() {
		defer wg.Done()
		gpuSharedLibPath := utils.GpuSharedLibPath
		if s := viper.GetString("gpu_shared_lib_path"); s != "" {
			gpuSharedLibPath = s
		}
		log.Info().Str("gpu_shared_lib_path", gpuSharedLibPath).Msg("Ensuring LibCedana library exists.")
		err = pullGPUBinary(ctx, utils.GpuSharedLibName, gpuSharedLibPath)
		if err != nil {
			log.Error().Err(err).Msg("could not download libcedana")
			return
		}
	}()

	wg.Wait()
	return err
}

func pullGPUBinary(ctx context.Context, binary string, filePath string) error {
	_, err := os.Stat(filePath)
	if err == nil {
		log.Info().Str("Path", filePath).Msgf("%s binary found, skipping download.", binary)
		// TODO NR - check version and checksum of binary?
		return nil
	}
	url := viper.GetString("connection.cedana_url") + "/k8s/gpu/" + binary
	log.Info().Msgf("Downloading %s from %s", binary, url)

	httpClient := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Err(err).Msg("failed to build http post request with jsonBody")
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			log.Error().Err(err).Int("status", resp.StatusCode).Msg("could not get gpu binary")
		} else {
			log.Error().Err(err).Msg("could not get gpu binary")
		}
		return fmt.Errorf("could not get gpu binary")
	}
	defer resp.Body.Close()

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err == nil {
		err = os.Chmod(filePath, 0755)
	}
	if err != nil {
		log.Err(err).Msg("could not create file")
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		log.Err(err).Msg("could not read file from response")
		return err
	}
	log.Info().Msgf("%s downloaded to %s", binary, filePath)
	return err
}

func payloadToJSON(payload any) string {
	if payload == nil {
		return "null"
	}

	protoMsg, ok := payload.(proto.Message)
	if !ok {
		return fmt.Sprintf("%+v", payload)
	}

	marshaler := protojson.MarshalOptions{
		EmitUnpopulated: true,
		Indent:          "  ",
	}
	jsonData, err := marshaler.Marshal(protoMsg)
	if err != nil {
		return fmt.Sprintf("Error marshaling to JSON: %v", err)
	}

	return string(jsonData)
}
