package main

import (
	"github.com/nravic/cedana/cmd"
	"github.com/nravic/cedana/utils"
)

func main() {
	utils.InitConfig()
	cmd.Execute()
}
