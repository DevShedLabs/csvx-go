package main

import (
	"encoding/json"
	"fmt"
	"os"

	csvx "github.com/csvx-org/csvx"
)

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}

	command := os.Args[1]
	filename := os.Args[2]
	workbook, err := csvx.Open(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "csvx %s: %v\n", command, err)
		os.Exit(1)
	}

	switch command {
	case "inspect":
		if err := printJSON(workbook); err != nil {
			fmt.Fprintf(os.Stderr, "csvx inspect: %v\n", err)
			os.Exit(1)
		}
	case "validate":
		fmt.Printf("valid: %s\n", filename)
	default:
		usage()
		os.Exit(2)
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: csvx <inspect|validate> <file.csvx>")
}
