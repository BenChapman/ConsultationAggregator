package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/adlio/trello"
)

func main() {
	configMap := getConfig()

	tc := trello.NewClient(configMap["trello_key"], configMap["trello_token"])
	board, err := tc.GetBoard(configMap["trello_board_id"], nil)
	if err != nil {
		fmt.Printf("board error: %s\n", err)
		os.Exit(1)
	}

	lists, err := board.GetLists(nil)
	if err != nil {
		fmt.Printf("could not get lists: %s\n", err)
		os.Exit(1)
	}

	list := &trello.List{}
	for _, v := range lists {
		if v.Name == configMap["trello_list_name"] {
			list = v
			break
		}
	}

	if list.ID == "" {
		fmt.Printf("Could not find list\n")
		os.Exit(1)
	}

	for _, v := range getOpenConsultations(configMap["citizen_space_instance_url"]) {
		endDate, err := time.Parse("2006/01/02", v["enddate"].(string))
		if err != nil {
			fmt.Printf("failed to parse date for submission %s: %s", v["title"].(string), err)
			continue
		}

		card := &trello.Card{
			Name: v["title"].(string),
			Desc: v["url"].(string),
			Due:  &endDate,
		}

		err = list.AddCard(card, nil)
		if err != nil {
			fmt.Printf("error creating card for %s: %s", v["title"].(string), err)
			continue
		}
	}
}

func getConfig() map[string]string {
	file, err := os.Open("config.json")
	if err != nil {
		fmt.Printf("could not get config: %s\n", err)
		os.Exit(1)
	}

	configMap := map[string]string{}
	err = json.NewDecoder(file).Decode(&configMap)
	if err != nil {
		fmt.Printf("could not decode config: %s", err)
		os.Exit(1)
	}

	return configMap
}

func getOpenConsultations(url string) []map[string]interface{} {
	result, err := http.Get(fmt.Sprintf("%s/api/2.3/json_search_results?dk=op&fd=2018/01/01&td=2018/12/31", url))
	if err != nil {
		fmt.Printf("error getting consultations: %s\n", err)
		os.Exit(1)
	}

	consultations := []map[string]interface{}{}
	err = json.NewDecoder(result.Body).Decode(&consultations)
	if err != nil {
		fmt.Printf("error decoding consultations: %s\n", err)
		os.Exit(1)
	}

	return consultations
}
