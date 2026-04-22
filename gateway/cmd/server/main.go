package main

import (
	"log"

	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
)

func main() {
	log.Fatal(apphttp.NewRouter().Listen(":8080"))
}
