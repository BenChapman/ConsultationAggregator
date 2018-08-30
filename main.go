package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/adlio/trello"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
)

type ConsultationAggregatorConfig struct {
	TrelloKey      string `json:"trello_key"`
	TrelloToken    string `json:"trello_token"`
	TrelloBoardID  string `json:"trello_board_id"`
	TrelloListName string `json:"trello_list_name"`
	Sources        []struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		Label string `json:"label"`
	} `json:"sources"`
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

	for _, v := range caConfig.Sources {
		switch v.Type {
		case "citizen_space":
			consultations = append(consultations, getOpenConsultationsFromCitizenSpace(v.Label, v.URL)...)
		case "civiq":
			consultations = append(consultations, getOpenConsultationsFromCiviqRSS(v.Label, v.URL)...)
		default:
			fmt.Printf("do not have source type %s", v.Type)
		}
	}

	for _, v := range consultations {
		label, err := getLabelByName(labels, v["label"].(string))
		if err != nil {
			fmt.Printf("label error: %s", err)
		}

		card := &trello.Card{
			IDBoard:  board.ID,
			IDList:   list.ID,
			Name:     v["title"].(string),
			Desc:     v["url"].(string),
			IDLabels: []string{label.ID},
		}
		endDate := v["enddate"].(time.Time)
		if !endDate.IsZero() {
			card.Due = &endDate
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

func getOpenConsultationsFromCitizenSpace(label string, url string) []map[string]interface{} {
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
		endDate := time.Time{}
		if _, ok := consultations[k]["enddate"]; ok {
			endDate, err = time.Parse("2006/01/02", consultations[k]["enddate"].(string))
			if err != nil {
				fmt.Printf("failed to parse date for submission %s: %s", consultations[k]["title"].(string), err)
			}
			consultations[k]["enddate"] = endDate
		}
	}

	return consultations
}

func getOpenConsultationsFromCiviqRSS(label string, feedURL string) []map[string]interface{} {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		fmt.Printf("error getting consultations: %s\n", err)
	}

	// Sadly the "link" field in the RSS is wrong, so we have to get the URL of the Civiq
	// instance in order to generate a working URL from the GUID of the item
	rssURL, err := url.Parse(feedURL)
	if err != nil {
		fmt.Printf("error parsing url: %s\n", err)
	}
	rssURL.Path = ""
	rssURL.RawPath = ""
	rssURL.RawQuery = ""
	rssURL.Fragment = ""

	consultations := []map[string]interface{}{}
	for _, v := range feed.Items {
		endDate, err := extractEndDateFromDescription(v.Description)
		if err != nil {
			fmt.Printf("error parsing date: %s\n", err)
		}

		consultations = append(consultations, map[string]interface{}{
			"label":   label,
			"title":   html.UnescapeString(v.Title),
			"url":     fmt.Sprintf("%s/en/node/%s", rssURL, v.GUID),
			"id":      v.GUID,
			"enddate": endDate,
		})
	}

	return consultations
}

func extractEndDateFromDescription(description string) (time.Time, error) {
	node, err := html.Parse(strings.NewReader(html.UnescapeString(description)))
	if err != nil {
		return time.Time{}, err
	}

	document := goquery.NewDocumentFromNode(node)

	endDateNode, ok := document.Find(".date-display-end").First().Attr("content")
	if !ok {
		return time.Time{}, nil
	}

	endDate, err := time.Parse("2006-01-02T15:04:05-07:00", endDateNode)

	return endDate, err
}

func getLabelByName(labels []*trello.Label, name string) (*trello.Label, error) {
	for _, v := range labels {
		if v.Name == name {
			return v, nil
		}
	}

	return nil, fmt.Errorf("could not find label %s in board", name)
}
