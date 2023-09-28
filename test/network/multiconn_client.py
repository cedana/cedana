# python script to emulate multiple concurrent connections 
# usage: python3 multiconn_client.py -n 3 google.com 80 (3 sockets, 3 connections to google.com:80)
import socket
import argparse
import threading
import time

def ping_server(ip, port):
    while True:
        try:
            client_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            client_socket.connect((ip, port))
            print(f"Connected to {ip}:{port}")
            
            # Add your logic here to send and receive data if needed
            
            # Sleep for a while before pinging again (e.g., every 5 seconds)
            time.sleep(5)
            
        except Exception as e:
            print(f"Error connecting to {ip}:{port}: {str(e)}")
            # Sleep for a while before trying to connect again (e.g., every 5 seconds)
            time.sleep(5)

def main():
    parser = argparse.ArgumentParser(description="Continuous TCP Server Pinger")
    parser.add_argument("ip", help="IP address of the server to ping")
    parser.add_argument("port", type=int, help="Port of the server to ping")
    parser.add_argument("-n", "--num-sockets", type=int, default=1,
                        help="Number of sockets to use for continuous pinging")
    
    args = parser.parse_args()

    ip = args.ip
    port = args.port
    num_sockets = args.num_sockets

    print(f"Continuous pinging {ip}:{port} with {num_sockets} sockets...")

    for _ in range(num_sockets):
        threading.Thread(target=ping_server, args=(ip, port)).start()

if __name__ == "__main__":
    main()