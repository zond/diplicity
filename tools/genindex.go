package main

import (
	"flag"
	"io/ioutil"
	"strings"
)

var (
	sortOrders = [][]string{
		{
			"StartETA",
		},
		{
			"-FinishedAt",
		},
		{
			"-StartedAt",
		},
	}
	fields = []string{
		"GameMasterId",
		"Started",
		"Closed",
		"Finished",
		"Variant",
		"Private",
		"DisableConferenceChat",
		"DisableGroupChat",
		"DisablePrivateChat",
		"NationAllocation",
		"Members.User.Id",
	}
)

func genIndex() []string {
	rval := []string{""}
	for _, order := range sortOrders {
		for _, field := range fields {
			rval = append(rval,
				"- kind: Game",
				"  properties:",
				"  - name: "+field,
			)
			for _, orderField := range order {
				desc := false
				if strings.Index(orderField, "-") == 0 {
					desc = true
					orderField = string(([]rune(orderField))[1:])
				}
				rval = append(rval,
					"  - name: "+orderField,
				)
				if desc {
					rval = append(rval,
						"    direction: desc",
					)
				}
			}
			rval = append(rval, "")
		}
	}
	rval = append(rval, "")
	return rval
}

func main() {
	indexFile := flag.String("index_file", "index.yaml", "Which file to add the generated indices to.")

	flag.Parse()

	b, err := ioutil.ReadFile(*indexFile)
	if err != nil {
		panic(err)
	}
	lines := strings.Split(string(b), "\n")
	newLines := make([]string, 0, len(lines))
	state := "copy"
	for _, line := range lines {
		if state == "copy" {
			newLines = append(newLines, line)
			if strings.TrimSpace(line) == "# GENERATED BY genindex.go" {
				newLines = append(newLines, genIndex()...)
				state = "ignore"
			}
		} else if state == "ignore" {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				newLines = append(newLines, line)
				state = "copy"
			}
		}
	}
	if err := ioutil.WriteFile(*indexFile, []byte(strings.Join(newLines, "\n")), 0600); err != nil {
		panic(err)
	}
}
