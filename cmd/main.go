package main

import (
	"encoding/json"
	scraper "github.com/BatteredBunny/youtube_scraper"
	"log"
)

func main() {
	c, _ := scraper.NewChannelScraper("@BitcoinMagazine")

	var printedChannel bool
	for {
		videos, err := c.NextShortsPage()
		if err != nil {
			log.Fatal(err)
		} else if len(videos) == 0 {
			break
		}

		if !printedChannel {
			if available, channel := c.GetChannelInfo(); available {
				bs, err := json.MarshalIndent(channel, "", "	")
				if err != nil {
					log.Fatal(err)
				}
				log.Println(string(bs))
			}

			printedChannel = true
		}

		for _, video := range videos {
			log.Println(video.VideoID, video.Title, video.Views)
		}
	}
}
