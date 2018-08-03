package docker

const (
	ACRegistry  = "appscode"
	ImageKubeci = "kubeci"
)

type Docker struct {
	Registry, Image, Tag string
}

func (docker Docker) ToContainerImage() string {
	return docker.Registry + "/" + docker.Image + ":" + docker.Tag
}