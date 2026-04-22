package main
import (
	"fmt"
	"path"
)
func main() {
	matched, _ := path.Match("/*", "/other")
	fmt.Printf("/* vs /other: %v\n", matched)
	matched, _ = path.Match("/*", "/other/path")
	fmt.Printf("/* vs /other/path: %v\n", matched)
}
