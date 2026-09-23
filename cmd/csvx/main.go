package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	csvx "github.com/csvx-org/csvx"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		printHelp(os.Stderr)
		os.Exit(2)
	}

	if isHelp(os.Args[1]) {
		printHelp(os.Stdout)
		return
	}
	if os.Args[1] == "version" {
		fmt.Printf("csvx %s\n", version)
		return
	}

	command := os.Args[1]
	if command != "inspect" && command != "validate" && command != "package" {
		fmt.Fprintf(os.Stderr, "csvx: unknown command %q\n\n", command)
		printHelp(os.Stderr)
		os.Exit(2)
	}

	if command == "package" {
		runPackage(os.Args[2:])
		return
	}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	help := flags.Bool("help", false, "show command help")
	if err := flags.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if *help {
		printCommandHelp(os.Stdout, command)
		return
	}
	if flags.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "csvx %s: expected exactly one input path\n\n", command)
		printCommandHelp(os.Stderr, command)
		os.Exit(2)
	}

	filename := flags.Arg(0)
	workbook, err := openInput(filename)
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
	}
}

func runPackage(arguments []string) {
	flags := flag.NewFlagSet("package", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	help := flags.Bool("help", false, "show command help")
	output := flags.String("output", "", "output .csvx file")
	if err := flags.Parse(arguments); err != nil {
		os.Exit(2)
	}
	if *help {
		printCommandHelp(os.Stdout, "package")
		return
	}
	if flags.NArg() != 1 || *output == "" {
		fmt.Fprintln(os.Stderr, "csvx package: provide an input directory and --output file.csvx")
		printCommandHelp(os.Stderr, "package")
		os.Exit(2)
	}
	if err := csvx.PackageDirectory(flags.Arg(0), *output); err != nil {
		fmt.Fprintf(os.Stderr, "csvx package: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("created: %s\n", *output)
}

func openInput(filename string) (*csvx.Workbook, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return csvx.OpenDirectory(filename)
	}
	return csvx.Open(filename)
}

func isHelp(argument string) bool {
	return argument == "--help" || argument == "-h" || argument == "help"
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printHelp(output *os.File) {
	fmt.Fprintln(output, "CSVX spreadsheet package tools")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  csvx <command> [options] <input>")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  inspect    Print the loaded workbook as JSON")
	fmt.Fprintln(output, "  validate   Load and validate a CSVX package")
	fmt.Fprintln(output, "  package    Package an unpacked directory into a .csvx ZIP file")
	fmt.Fprintln(output, "  version    Print the CLI version")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Input may be a .csvx ZIP file or an unpacked CSVX directory.")
	fmt.Fprintln(output, "Use 'csvx <command> --help' for command-specific help.")
}

func printCommandHelp(output *os.File, command string) {
	switch command {
	case "inspect":
		fmt.Fprintln(output, "Usage: csvx inspect [--help] <input>")
		fmt.Fprintln(output, "")
		fmt.Fprintln(output, "Loads a CSVX ZIP file or unpacked package directory and prints the workbook as JSON.")
	case "validate":
		fmt.Fprintln(output, "Usage: csvx validate [--help] <input>")
		fmt.Fprintln(output, "")
		fmt.Fprintln(output, "Loads a CSVX ZIP file or unpacked package directory and reports whether it is valid.")
	case "package":
		fmt.Fprintln(output, "Usage: csvx package [--help] --output <file.csvx> <directory>")
		fmt.Fprintln(output, "")
		fmt.Fprintln(output, "Packages an unpacked CSVX directory into a ZIP-based .csvx file.")
	}
}
