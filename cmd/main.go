package main

import (
	"encoding/json"
	scraper "github.com/0x090909/youtube_scraper"
	"log"
)

func main() {
	//scraper.HLCode = "zh-CN"
	c, err := scraper.NewChannelScraper("@cnliziqi")
	if err != nil {
		log.Fatal("NewChannelScraper ", err)
	}

	var printedChannel bool
	for {
		videos, err := c.NextVideosPage()
		if err != nil {
			log.Fatal("NextVideosPage ", err)
		} else if len(videos) == 0 {
			break
		}

		if !printedChannel {
			if available, channel := c.GetChannelInfo(); available {
				bs, err := json.MarshalIndent(channel, "", "	")
				if err != nil {
					log.Fatal("json.MarshalIndent ", err)
				}
				log.Println(string(bs))
			}

			printedChannel = true
		}

		for _, video := range videos {
			//info, err := video2.NewVideoWatchInfo(video.VideoID)
			//if err != nil {
			//	log.Fatal("NewVideoWatchInfo ", err)
			//	return
			//}
			//log.Fatal(info.VideoID, info.Title, info.ViewCount, info.Thumbnail, info.UploadDate)
			log.Println(video.VideoID, video.Title, video.Views)
		}
	}
}
