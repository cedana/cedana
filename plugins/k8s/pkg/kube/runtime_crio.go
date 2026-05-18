package kube

type CrioClient struct {
	RuntimeClientUnimplemented
}

func NewCrioClient() (*CrioClient, error) {
	return &CrioClient{}, nil
}

func (c *CrioClient) String() string {
	return "crio"
}
