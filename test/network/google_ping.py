import socket
import time

def tcp_client():
    while True:
        server_ip = "216.58.200.46"  # One of Google's IPs
        port = 80  # HTTP port

        client_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        client_socket.connect((server_ip, port))
        client_socket.settimeout(5)

        get_request = "GET / HTTP/1.1\r\nHost: www.google.com\r\n\r\n"
        client_socket.sendall(get_request.encode())

        data = client_socket.recv(1024)
        print(f"Received {len(data)} bytes.")

        time.sleep(1)  # Sleep for 1 second before repeating

if __name__ == "__main__":
    tcp_client()