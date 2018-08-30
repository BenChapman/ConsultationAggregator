package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/adlio/trello"
)

type ConsultationAggregatorConfig struct {
	TrelloKey             string `json:"trello_key"`
	TrelloToken           string `json:"trello_token"`
	TrelloBoardID         string `json:"trello_board_id"`
	TrelloListName        string `json:"trello_list_name"`
	CitizenSpaceInstances []struct {
		URL   string `json:"url"`
		Label string `json:"label"`
	} `json:"citizen_space_instances"`
}

type CacheItems []string

func (ci CacheItems) Contains(id string) bool {
	for _, v := range ci {
		if v == id {
			return true
		}
	}
	return false
}

func main() {
	caConfig := getConfig()

	cacheFilePath := filepath.Join(os.Getenv("HOME"), ".ConsultationCache")
	file, err := os.Open(cacheFilePath)
	if err != nil {
		fmt.Printf("cache file error: %s\n", err)
		os.Exit(1)
	}

	cache := CacheItems{}
	err = json.NewDecoder(file).Decode(&cache)
	if err != nil {
		fmt.Printf("cache decode error: %s\n", err)
		os.Exit(1)
	}
	file.Close()

	tc := trello.NewClient(caConfig.TrelloKey, caConfig.TrelloToken)
	board, err := tc.GetBoard(caConfig.TrelloBoardID, nil)
	if err != nil {
		fmt.Printf("board error: %s\n", err)
		os.Exit(1)
	}

	labels, err := board.GetLabels(nil)
	if err != nil {
		fmt.Printf("label error: %s\n", err)
		os.Exit(1)
	}

	lists, err := board.GetLists(nil)
	if err != nil {
		fmt.Printf("could not get lists: %s\n", err)
		os.Exit(1)
	}

	list := &trello.List{}
	for _, v := range lists {
		if v.Name == caConfig.TrelloListName {
			list = v
			break
		}
	}

	if list.ID == "" {
		fmt.Printf("Could not find list\n")
		os.Exit(1)
	}

	consultations := []map[string]interface{}{}

	for _, v := range caConfig.CitizenSpaceInstances {
		consultations = append(consultations, getOpenConsultations(v.Label, v.URL)...)
	}

	for _, v := range consultations {
		endDate, err := time.Parse("2006/01/02", v["enddate"].(string))
		if err != nil {
			fmt.Printf("failed to parse date for submission %s: %s", v["title"].(string), err)
			continue
		}

		label, err := getLabelByName(labels, v["label"].(string))
		if err != nil {
			fmt.Printf("label error: %s", err)
		}

		card := &trello.Card{
			IDBoard:  board.ID,
			IDList:   list.ID,
			Name:     v["title"].(string),
			Desc:     v["url"].(string),
			Due:      &endDate,
			IDLabels: []string{label.ID},
		}

		if !cache.Contains(v["id"].(string)) {
			// For some reason creating the card using the List.AddCard() method
			// means the labels don't get added. Instead use the client to create the
			// card
			err = tc.CreateCard(card, nil)
			if err != nil {
				fmt.Printf("error creating card for %s: %s", v["title"].(string), err)
				continue
			}

			cache = append(cache, v["id"].(string))
		}
	}

	file, err = os.OpenFile(cacheFilePath, os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("cache file error: %s\n", err)
		os.Exit(1)
	}

	err = json.NewEncoder(file).Encode(cache)
	if err != nil {
		fmt.Printf("cache encoding error: %s\n", err)
		os.Exit(1)
	}
}

func getConfig() ConsultationAggregatorConfig {
	file, err := os.Open("config.json")
	if err != nil {
		fmt.Printf("could not get config: %s\n", err)
		os.Exit(1)
	}

	caConfig := ConsultationAggregatorConfig{}
	err = json.NewDecoder(file).Decode(&caConfig)
	if err != nil {
		fmt.Printf("could not decode config: %s", err)
		os.Exit(1)
	}

	return caConfig
}

func getOpenConsultations(label string, url string) []map[string]interface{} {
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

	for k := range consultations {
		consultations[k]["label"] = label
	}

	return consultations
}

func getLabelByName(labels []*trello.Label, name string) (*trello.Label, error) {
	for _, v := range labels {
		if v.Name == name {
			return v, nil
		}
	}

	return nil, fmt.Errorf("could not find label %s in board", name)
}
