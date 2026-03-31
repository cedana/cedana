#include <stdio.h>
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>
#include <string.h>
#include <errno.h>

int main() {
    int fd_ctl;

    printf("Opening /dev/nvidiactl...\n");
    fd_ctl = open("/dev/nvidiactl", O_RDWR);
    if (fd_ctl < 0) {
        fprintf(stderr, "Failed to open /dev/nvidiactl: %s\n", strerror(errno));
        fprintf(stderr, "This is expected if no NVIDIA GPU is present. Test will simulate with dummy behavior.\n");
    } else {
        printf("Successfully opened /dev/nvidiactl (fd=%d)\n", fd_ctl);
    }

    printf("PID: %d\n", getpid());
    printf("Keeping /dev/nvidiactl open. Press Ctrl+C to exit or checkpoint now.\n");
    printf("You can now try: cedana dump --pid %d\n", getpid());

    // Sleep forever keeping the file descriptor open
    while (1) {
        sleep(60);
    }

    // Cleanup (never reached in normal flow)
    if (fd_ctl >= 0) close(fd_ctl);

    return 0;
}
