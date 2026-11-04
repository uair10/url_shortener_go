// Command openapi пишет спецификацию OpenAPI в файл.
package main

import (
	"flag"
	"log"
	"os"

	"github.com/uair10/url_shortener_go/internal/httpapi"
)

// filePerm - права файла спецификации. git хранит только бит исполнения,
// в репозитории файл всё равно будет 0644.
const filePerm os.FileMode = 0o600

func main() {
	out := flag.String("o", "api/openapi.yaml", "output file")
	flag.Parse()

	b, err := httpapi.OpenAPIYAML()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, b, filePerm); err != nil {
		log.Fatal(err)
	}
}
