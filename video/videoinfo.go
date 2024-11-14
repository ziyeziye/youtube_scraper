package video

import (
	"bytes"
	"errors"
	scraper "github.com/0x090909/youtube_scraper"
	"github.com/BatteredBunny/rjson"
	"github.com/PuerkitoBio/goquery"
	"github.com/golang-module/carbon/v2"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

type VideoInfo struct {
	VideoID           string `rjson:"videoDetails.videoId"`
	ChannelID         string `rjson:"videoDetails.channelId"`
	ChannelTitle      string `rjson:"videoDetails.author"`
	Title             string `rjson:"videoDetails.title"`
	Description       string `rjson:"videoDetails.shortDescription"`
	PublishDate       string
	UploadDate        string
	PublishDateStr    string   `rjson:"microformat.playerMicroformatRenderer.publishDate"`
	UploadDateStr     string   `rjson:"microformat.playerMicroformatRenderer.uploadDate"`
	Category          string   `rjson:"microformat.playerMicroformatRenderer.category"`
	ViewCount         int      `rjson:"videoDetails.viewCount"`
	VideoDuration     int      `rjson:"videoDetails.lengthSeconds"`
	Keywords          []string `rjson:"videoDetails.keywords"`
	RegionsAllowed    []string `rjson:"microformat.playerMicroformatRenderer.availableCountries"`
	Thumbnail         string   `rjson:"microformat.playerMicroformatRenderer.thumbnail.thumbnails[0].url"`
	IsLive            bool     `rjson:"videoDetails.isLiveContent"`
	IsOwnerViewing    bool     `rjson:"videoDetails.isOwnerViewing"`
	IsCrawlable       bool     `rjson:"videoDetails.isCrawlable"`
	AllowRatings      bool     `rjson:"videoDetails.allowRatings"`
	IsPrivate         bool     `rjson:"videoDetails.isPrivate"`
	IsUnpluggedCorpus bool     `rjson:"videoDetails.isUnpluggedCorpus"`
}

func NewVideoWatchInfo(id string) (info VideoInfo, err error) {
	body, err := fetchWatchBody(id)
	if err != nil {
		return
	}

	rawJson, err := getPlayerResponse(body)
	if err != nil {
		return
	}
	scraper.DebugFileOutput([]byte(rawJson), "video_watch_initial.json")

	//rjson.Debug = true
	if err = rjson.Unmarshal([]byte(rawJson), &info); err != nil {
		if errors.Is(err, rjson.ErrCantFindField) {
			if scraper.Debug {
				log.Println("WARNING:", err)
			}
			err = nil
		}
		return
	}

	if info.Thumbnail == "" {
		info.Thumbnail = "https://i.ytimg.com/vi/" + info.VideoID + "/maxresdefault.jpg"
	}

	info.PublishDate = carbon.Parse(info.PublishDateStr).ToDateString()
	info.UploadDate = carbon.Parse(info.UploadDateStr).ToDateString()

	return
}

func fetchWatchBody(id string) ([]byte, error) {
	rawUrl, err := url.Parse("https://www.youtube.com/watch")
	if err != nil {
		return nil, nil
	}

	q := rawUrl.Query()
	q.Set("v", id)
	q.Set("hl", scraper.HLCode)
	q.Set("has_verified", "1")
	q.Set("bpctr", "9999999999")
	rawUrl.RawQuery = q.Encode()
	//v.url = rawUrl.String()

	resp, err := http.Get(rawUrl.String())
	if err != nil {
		return nil, err
	}

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func getPlayerResponse(body []byte) (rawJson string, err error) {
	var doc *goquery.Document
	doc, err = goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return
	}

	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		if cut, valid := strings.CutPrefix(s.Text(), "var ytInitialPlayerResponse ="); valid {
			rawJson, _ = strings.CutSuffix(cut, ";")
		}
	})

	return
}

func GetThumbnails(id string) map[string]string {
	return map[string]string{
		"default":  "https://img.youtube.com/vi/" + id + "/default.jpg",
		"medium":   "https://img.youtube.com/vi/" + id + "/mqdefault.jpg",
		"high":     "https://img.youtube.com/vi/" + id + "/hqdefault.jpg",
		"standard": "https://img.youtube.com/vi/" + id + "/sddefault.jpg",
		"maxres":   "https://img.youtube.com/vi/" + id + "/maxresdefault.jpg",
	}

}
