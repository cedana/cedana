package main

import (
	"github.com/nravic/cedana-client/cmd"
	"github.com/nravic/cedana-client/utils"
)

func main() {
	utils.InitConfig()
	cmd.Execute()
}
