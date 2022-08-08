package main

import (
	"github.com/nravic/oort/cmd"
	"github.com/nravic/oort/utils"
)

func main() {
	utils.InitConfig()
	cmd.Execute()
}
