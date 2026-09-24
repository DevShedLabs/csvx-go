package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	csvx "github.com/DevShedLabs/csvx-go"
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
	if command != "inspect" && command != "validate" && command != "package" && command != "extract" && command != "xlsx-inspect" {
		fmt.Fprintf(os.Stderr, "csvx: unknown command %q\n\n", command)
		printHelp(os.Stderr)
		os.Exit(2)
	}

	if command == "package" {
		runPackage(os.Args[2:])
		return
	}
	if command == "extract" {
		runExtract(os.Args[2:])
		return
	}
	if command == "xlsx-inspect" {
		runXLSXInspect(os.Args[2:])
		return
	}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	help := flags.Bool("help", false, "show command help")
	jsonOutput := flags.Bool("json", false, "write machine-readable JSON")
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
		result := csvx.Validate(filename)
		if *jsonOutput {
			if err := printJSON(result); err != nil {
				fmt.Fprintf(os.Stderr, "csvx validate: %v\n", err)
				os.Exit(1)
			}
			if !result.Valid {
				os.Exit(1)
			}
			return
		}
		if !result.Valid {
			fmt.Fprintf(os.Stderr, "invalid: %s\n", filename)
			for _, diagnostic := range result.Errors {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", diagnostic.Code, diagnostic.Message)
			}
			os.Exit(1)
		}
		fmt.Printf("valid: %s\n", filename)
	}
}

func runPackage(arguments []string) {
	input, output, showHelp, err := parsePackageArguments(arguments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "csvx package: %v\n\n", err)
		printCommandHelp(os.Stderr, "package")
		os.Exit(2)
	}
	if showHelp {
		printCommandHelp(os.Stdout, "package")
		return
	}
	if err := csvx.PackageDirectory(input, output); err != nil {
		fmt.Fprintf(os.Stderr, "csvx package: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("created: %s\n", output)
}

func parsePackageArguments(arguments []string) (string, string, bool, error) {
	var input, output string
	for index := 0; index < len(arguments); index++ {
		switch arguments[index] {
		case "--help", "-h":
			return "", "", true, nil
		case "--output", "-o":
			if index+1 >= len(arguments) {
				return "", "", false, fmt.Errorf("--output requires a file path")
			}
			output = arguments[index+1]
			index++
		default:
			if input != "" {
				return "", "", false, fmt.Errorf("expected one input directory, got %q", arguments[index])
			}
			input = arguments[index]
		}
	}
	if input == "" || output == "" {
		return "", "", false, fmt.Errorf("provide an input directory and --output file.csvx")
	}
	return input, output, false, nil
}

func runExtract(arguments []string) {
	input, output, showHelp, err := parseExtractArguments(arguments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "csvx extract: %v\n\n", err)
		printCommandHelp(os.Stderr, "extract")
		os.Exit(2)
	}
	if showHelp {
		printCommandHelp(os.Stdout, "extract")
		return
	}
	if err := csvx.ExtractPackage(input, output); err != nil {
		fmt.Fprintf(os.Stderr, "csvx extract: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("extracted: %s\n", output)
}

func parseExtractArguments(arguments []string) (string, string, bool, error) {
	var input, output string
	for index := 0; index < len(arguments); index++ {
		switch arguments[index] {
		case "--help", "-h":
			return "", "", true, nil
		case "--output", "-o":
			if index+1 >= len(arguments) {
				return "", "", false, fmt.Errorf("--output requires a directory path")
			}
			output = arguments[index+1]
			index++
		default:
			if input != "" {
				return "", "", false, fmt.Errorf("expected one input package, got %q", arguments[index])
			}
			input = arguments[index]
		}
	}
	if input == "" || output == "" {
		return "", "", false, fmt.Errorf("provide an input .csvx file and --output directory")
	}
	return input, output, false, nil
}

func runXLSXInspect(arguments []string) {
	flags := flag.NewFlagSet("xlsx-inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "write machine-readable JSON")
	if err := flags.Parse(arguments); err != nil { os.Exit(2) }
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "csvx xlsx-inspect: expected exactly one input .xlsx path")
		printCommandHelp(os.Stderr, "xlsx-inspect")
		os.Exit(2)
	}
	inspection, err := csvx.InspectXLSX(flags.Arg(0))
	if err != nil { fmt.Fprintf(os.Stderr, "csvx xlsx-inspect: %v\n", err); os.Exit(1) }
	if *jsonOutput { if err := printJSON(inspection); err != nil { fmt.Fprintf(os.Stderr, "csvx xlsx-inspect: %v\n", err); os.Exit(1) }; return }
	fmt.Printf("XLSX: %s\nSHA-256: %s\nSheets: %d\nResources: %d\n", inspection.Filename, inspection.SHA256, len(inspection.Sheets), len(inspection.Resources))
	for _, warning := range inspection.Warnings { fmt.Printf("[%s] %s: %s\n", warning.Severity, warning.Feature, warning.Message) }
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
	fmt.Fprintln(output, "  extract    Extract a .csvx ZIP file into an unpacked directory")
	fmt.Fprintln(output, "  xlsx-inspect Inspect an XLSX package and report detected features")
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
		fmt.Fprintln(output, "Use --json for machine-readable diagnostics.")
	case "package":
		fmt.Fprintln(output, "Usage: csvx package [--help] --output <file.csvx> <directory>")
		fmt.Fprintln(output, "")
		fmt.Fprintln(output, "Packages an unpacked CSVX directory into a ZIP-based .csvx file.")
	case "extract":
		fmt.Fprintln(output, "Usage: csvx extract [--help] --output <directory> <file.csvx>")
		fmt.Fprintln(output, "")
		fmt.Fprintln(output, "Extracts a ZIP-based .csvx file into an unpacked directory for inspection or editing.")
	case "xlsx-inspect":
		fmt.Fprintln(output, "Usage: csvx xlsx-inspect [--json] <file.xlsx>")
		fmt.Fprintln(output, "")
		fmt.Fprintln(output, "Inspects an XLSX package without executing macros or external links.")
	}
}
