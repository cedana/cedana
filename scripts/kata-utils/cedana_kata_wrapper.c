#include <stdlib.h>
#include <unistd.h>
#include <stddef.h>
#include <fcntl.h>

int main(int argc, char *argv[]) {
    char *copy_argv[argc];
    for(int i=0;i<argc;i++)
        copy_argv[i] = argv[i+1];

    int fd = open("container_io.txt", O_RDWR | O_CREAT, 0666);
    dup2(fd, STDOUT_FILENO);
    dup2(fd, STDIN_FILENO);
    dup2(fd, STDERR_FILENO);
    close(fd);

    execvpe(argv[1], copy_argv, NULL);
}