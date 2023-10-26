# script to run a server or client
import socket
import json
import time
import argparse

def run_server(port=4266):
    server_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server_socket.bind(("0.0.0.0", port))
    server_socket.listen(1)

    print(f"Listening for connections on port {port}")

    while True:
        client_socket, address = server_socket.accept()
        print(f"Accepted connection from {address}")

        try:
            while True:
                data = client_socket.recv(1024)
                if not data:
                    break

                payload = json.loads(data.decode("utf-8"))

                print(f"Received data: {payload}")

                response = {"status": "Received"}
                client_socket.send(json.dumps(response).encode("utf-8"))
        except Exception as e:
            print(f"Error occurred: {e}")
        finally:
            client_socket.close()

def run_client(server_ip="127.0.0.1", port=4266):
    client_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    client_socket.connect((server_ip, port))
    while True:
        payload = {"data": "Test", "timestamp": time.time()}
        client_socket.send(json.dumps(payload).encode("utf-8"))

        response = client_socket.recv(1024)
        print(f"Server response: {json.loads(response.decode('utf-8'))}")

        time.sleep(2)

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Run as a client or server.")
    parser.add_argument("--mode", type=str, required=True, choices=["client", "server"], help="Run as client or server")
    args = parser.parse_args()

    if args.mode == "client":
        run_client()
    elif args.mode == "server":
        run_server()