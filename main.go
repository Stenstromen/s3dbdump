package main

import (
	"log"

	"github.com/stenstromen/s3dbdump/mydump"
)

func main() {
	log.Printf("Starting s3dbdump")
	mydump.InitConfig()
	mydump.HandleDbDump(mydump.Config)
}
