package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type Root struct {
	XMLName    xml.Name   `xml:"Root"`
	CdsPayload CdsPayload `xml:"cds_payload"`
}

type CdsPayload struct {
	Files []File `xml:"file"`
}

type File struct {
	Name   string   `xml:"name,attr"`
	Base64 []string `xml:"base64"`
}

// helper function to print errors with line numbers
func printError(err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("Error at %s:%d: %v\n", filepath.Base(file), line, err)
	}
}

func main() {
	// Parse command-line arguments
	privateKeyPath := flag.String("private-key", "", "Path to the private key file for GnuPG decryption")
	flag.Parse()

	// Check if the private key path is provided
	if *privateKeyPath == "" {
		printError(fmt.Errorf("the --private-key argument is required"))
		return
	}

	// Find the folder that starts with "ISD" in the current directory
	folders, err := filepath.Glob("ISD*")
	if err != nil || len(folders) == 0 {
		printError(fmt.Errorf("no folder starting with 'ISD' found in the current directory"))
		return
	}
	folder := folders[0] // Use the first matching folder

	// Find the head XML file (without an underscore in the name)
	xmlFiles, err := filepath.Glob(filepath.Join(folder, "*.xml"))
	if err != nil || len(xmlFiles) == 0 {
		printError(fmt.Errorf("no XML files found in the folder: %s", folder))
		return
	}

	var headFile string
	for _, file := range xmlFiles {
		if !strings.Contains(filepath.Base(file), "_") {
			headFile = file
			break
		}
	}

	if headFile == "" {
		printError(fmt.Errorf("no head file found (file without underscore in name)"))
		return
	}

	// Read the head XML file
	xmlData, err := ioutil.ReadFile(headFile)
	if err != nil {
		printError(fmt.Errorf("reading head XML file: %s", headFile))
		return
	}

	// Unmarshal the XML data from the head file
	var headRoot Root
	err = xml.Unmarshal(xmlData, &headRoot)
	if err != nil {
		printError(fmt.Errorf("unmarshaling head XML: %s", headFile))
		return
	}

	// Check if the <base64> content starts with "filename" to identify the header file
	isHeaderFile := false
	var outputFileName string
	for _, file := range headRoot.CdsPayload.Files {
		if len(file.Base64) > 0 && strings.HasPrefix(file.Base64[0], "filename") {
			isHeaderFile = true
			outputFileName = file.Name
			break
		}
	}

	var concatenatedData strings.Builder
	if isHeaderFile {
		// Handle case 1: Header file with child files
		if outputFileName == "" {
			printError(fmt.Errorf("no output file name specified in the head file"))
			return
		}

		// Find all child XML files with an underscore and sort them by the number after the underscore
		var childFiles []string
		for _, file := range xmlFiles {
			baseName := filepath.Base(file)
			if strings.Contains(baseName, "_") {
				childFiles = append(childFiles, file)
			}
		}

		if len(childFiles) == 0 {
			printError(fmt.Errorf("no child files found with an underscore in the name"))
			return
		}

		// Sort the child files based on the number after the underscore
		sort.Slice(childFiles, func(i, j int) bool {
			getNum := func(filename string) int {
				parts := strings.Split(strings.TrimSuffix(filename, ".xml"), "_")
				if len(parts) < 2 {
					return 0
				}
				num, err := strconv.Atoi(parts[1])
				if err != nil {
					return 0
				}
				return num
			}
			return getNum(filepath.Base(childFiles[i])) < getNum(filepath.Base(childFiles[j]))
		})

		// Concatenate <base64> data from child files in the sorted order
		for _, childFile := range childFiles {
			// Read the child XML file
			childXMLData, err := ioutil.ReadFile(childFile)
			if err != nil {
				printError(fmt.Errorf("reading child XML file: %s", childFile))
				return
			}

			// Unmarshal the child XML file
			var childRoot Root
			err = xml.Unmarshal(childXMLData, &childRoot)
			if err != nil {
				printError(fmt.Errorf("unmarshaling child XML: %s", childFile))
				return
			}

			// Append the <base64> data from the child file
			for _, file := range childRoot.CdsPayload.Files {
				for _, base64Data := range file.Base64 {
					concatenatedData.WriteString(base64Data)
				}
			}
		}
	} else {
		// Handle case 2: Not a header file, process data within the same XML file
		// Concatenate <base64> data from the head file itself
		for _, file := range headRoot.CdsPayload.Files {
			for _, base64Data := range file.Base64 {
				concatenatedData.WriteString(base64Data)
			}
			// Use the file's name attribute as the output file name
			outputFileName = file.Name
		}

		if outputFileName == "" {
			printError(fmt.Errorf("no output file name specified in the head file"))
			return
		}
	}

	// Save the concatenated data to the specified output file
	err = ioutil.WriteFile(outputFileName, []byte(concatenatedData.String()), 0644)
	if err != nil {
		printError(fmt.Errorf("writing to file: %s", outputFileName))
		return
	}

	fmt.Printf("Data concatenated and saved to %s\n", outputFileName)

	// Use GnuPG to decrypt the output file with the provided private key
	gpgCommand := exec.Command("gpg", "--decrypt", "--batch", "--yes", "--output", strings.TrimSuffix(outputFileName, ".pgp"), "--passphrase-file", *privateKeyPath, outputFileName)
	gpgCommand.Stdin = os.Stdin
	gpgCommand.Stdout = os.Stdout
	gpgCommand.Stderr = os.Stderr

	// Run the GnuPG command
	err = gpgCommand.Run()
	if err != nil {
		printError(fmt.Errorf("GnuPG decryption failed: %v", err))
		return
	}

	fmt.Printf("Decryption completed and saved to %s\n", strings.TrimSuffix(outputFileName, ".pgp"))
}
