package cmd

import (
	"fmt"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/urfave/cli/v2"
	"io/ioutil"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

var cmdDump = &cli.Command{
	Name:      "dump",
	Aliases:   []string{"d"},
	Usage:     "Dump an entire index to STDOUT",
	UsageText: "elasticblaster dump SERVER INDEX",
	Action:    execDump,
}

var settingsToRemove = []string{"settings.index.creation_date", "settings.index.uuid", "settings.index.version", "settings.index.provided_name"}

func execDump(c *cli.Context) error {
	rand.Seed(time.Now().UnixNano())

	if c.NArg() < 2 {
		return cli.Exit("invalid syntax: ES host and/or index missing", 1)
	}

	host := c.Args().Get(0)
	index := c.Args().Get(1)
	rootURI := fmt.Sprintf("http://%s", host)
	client := &http.Client{}

	// Dump mapping first
	rootIndexURI := fmt.Sprintf("http://%s/%s", host, index)
	req, err := http.NewRequest("GET", rootIndexURI, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	rawMapping, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	mapping := gjson.GetBytes(rawMapping, index).String()
	for _, keyToRemove := range settingsToRemove {
		mapping, err = sjson.Delete(mapping, keyToRemove)
		if err != nil {
			return err
		}
	}
	fmt.Fprintln(c.App.Writer, mapping)

	// Initial search request
	uri := fmt.Sprintf("%s/_search?scroll=1m", rootIndexURI)
	req, err = http.NewRequest("POST", uri, strings.NewReader(`{"size":10000}`))
	if err != nil {
		return err
	}
	resp, err = client.Do(req)
	if err != nil {
		return err
	}

	if resp.Body == nil {
		return err
	}

	for {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		scrollID := gjson.GetBytes(body, "_scroll_id")
		if !scrollID.Exists() {
			return cli.Exit("no scroll id: " + string(body), 1)
		}

		hits := gjson.GetBytes(body, "hits.hits")
		if !hits.Exists() {
			panic("no hits")
		}

		if !hits.IsArray() {
			panic("no hits array")
		}

		if len(hits.Array()) == 0 {
			fmt.Fprintln(c.App.ErrWriter, "done")
			break
		}

		for _, hit := range hits.Array() {
			fmt.Fprintln(c.App.Writer, hit.Raw)
		}

		uri := fmt.Sprintf("%s/_search/scroll", rootURI)
		postBody := fmt.Sprintf(`{"scroll":"1m","scroll_id":"%s"}`, scrollID.String())
		req, err := http.NewRequest("POST", uri, strings.NewReader(postBody))
		if err != nil {
			return err
		}

		resp, err = client.Do(req)
		if err != nil {
			return err
		}

		if resp.Body == nil {
			return err
		}
	}
	return nil
}
