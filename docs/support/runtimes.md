# Container Runtime Support

Using the process as a primitive, cedana supports multiple container runtimes for checkpoint/restore. As we continue to abstract away the concept of what a container is (cgroups, overlay, namespaces, etc.) we'll continue to add support for more runtimes as well as increase support for currently supported ones. 

## Running containers 

The container ecosystem can get confusing, especially when dealing with runtimes. Currently, cedana only supports containers using the `runc` low-level runtime. Below are the ones that we have tested with and support working with.

| runtime/abstraction         | level of support | well-tested? | notes                                                                                             |
|-----------------------------|------------------|--------------|---------------------------------------------------------------------------------------------------|
| process                     | full             | yes          | simplest abstraction level, works with everything                                                 |
| runc                        | full             | yes          | simplest abstraction level, all other management layers need to be using runc                     |
| containerd (runc + rootfs)  | full             | yes          | works well, works in kubernetes                                                                   |
| kata containers             | experimental     | no           | works with cedana, but need to increase test coverage                                             |
| sysbox + crio (rootfs only) | full             | yes          | sysbox virtualization of proc and use of systemd makes process-level checkpoint/restore difficult |
| docker                      | mid              | no           | should just work                                                                                  |
| podman                      | mid              | no           | should just work, but untested in a little bit                                                    |

## Container rootfs 

