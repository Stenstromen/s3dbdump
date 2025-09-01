package main

import (
	"log"
	"runtime"

	"github.com/stenstromen/s3dbdump/mydump"
)

func main() {
	log.Printf("Starting s3dbdump")
	runtime.SetDefaultGOMAXPROCS()

	mydump.InitConfig()
	mydump.TestConnections()
	mydump.HandleDbDump(mydump.Config)
}
