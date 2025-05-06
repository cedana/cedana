openapi-generator-cli generate -i cloud-hypervisor.yaml -g go -o client
rm client/go.mod
rm -r client/test
go fmt
