# Container runtime support

Using the process as a primitive, cedana supports multiple container runtimes for checkpoint/restore. Each runtime support is available through its own plugin. We'll continue to create plugins for more runtimes as well as increase support for currently supported ones. 

| runtime/abstraction         | level of support | well-tested? | notes                                                                                             |
|-----------------------------|------------------|--------------|---------------------------------------------------------------------------------------------------|
| process                     | full             | yes          | simplest abstraction level, works with everything                                                 |
| runc                        | full             | yes          | simplest abstraction level, all other management layers need to be using runc                     |
| containerd (runc + rootfs)  | full             | yes          | works well, works in kubernetes                                                                   |
| kata containers             | experimental     | no           | works with cedana, but need to increase test coverage                                             |
| sysbox + crio (rootfs only) | full             | yes          | sysbox virtualization of proc and use of systemd makes process-level checkpoint/restore difficult |
| docker                      | mid              | no           | should just work                                                                                  |
| podman                      | mid              | no           | should just work, but untested in a little bit                                                    |
