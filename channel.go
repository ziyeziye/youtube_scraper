package scraper

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ayes-web/rjson"
	"github.com/dustin/go-humanize"
)

type Channel struct {
	Subscribers      int
	IsVerified       bool
	IsVerifiedArtist bool
	ChannelID        string
	NewChannelID     string
	Username         string
	Description      string
	VideosAmount     int

	Avatars []YoutubeImage
	Banners []YoutubeImage
}

type Video struct {
	VideoID string
	Title   string

	// Will be empty if its livestream
	// example value 7:03
	Length string `json:"Length,omitempty"`

	Views   int `json:"Views,omitempty"`   // Will be empty if its livestream
	Viewers int `json:"Viewers,omitempty"` // Empty if it's not a livestream

	/*
		Will be empty if its livestream

		Years
			- 2-11 years ago
			- 1 year ago

		Months
			- 2-11 months ago
			- 1 month ago

		Weeks
			- 2-4 weeks ago

		Days
			- 2-13 days ago
			- 1 day ago

		Hours
			- 2-23 hours ago
			- 1 hour ago

		Minutes
			- 2-59 minutes ago
			- 1 minute ago

		Seconds
			- 2-59 seconds ago
			- 1 second ago
	*/
	Date string `json:"Date,omitempty"`

	Username      string
	ChannelID     string
	NewChannelID  string // @username
	ChannelAvatar string

	IsLive                 bool
	WasLive                bool
	AuthorIsVerified       bool
	AuthorIsVerifiedArtist bool
}

type ChannelScraper struct {
	streamsUrl string
	videosUrl  string
	channel    Channel

	videosInitialComplete   bool
	videosContinueInputJson []byte

	streamsInitialComplete   bool
	streamsContinueInputJson []byte
}

// NewChannelScraper accepts normal id or @username
func NewChannelScraper(id string) (c ChannelScraper, err error) {
	rawUrl, err := url.Parse("https://www.youtube.com/")
	if err != nil {
		return
	}

	if strings.HasPrefix(id, "@") {
		rawUrl = rawUrl.JoinPath(id)
	} else {
		rawUrl = rawUrl.JoinPath("channel", id)
	}

	rawVideosUrl := rawUrl.JoinPath("videos")
	q := rawVideosUrl.Query()
	q.Set("hl", "en")
	rawVideosUrl.RawQuery = q.Encode()
	c.videosUrl = rawVideosUrl.String()

	rawStreamsUrl := rawUrl.JoinPath("streams")
	q = rawStreamsUrl.Query()
	q.Set("hl", "en")
	rawStreamsUrl.RawQuery = q.Encode()
	c.streamsUrl = rawStreamsUrl.String()

	return
}

// GetChannelInfo will output the internal channel struct which will become available after the first call to NextVideosPage() or NextStreamsPage()
func (c *ChannelScraper) GetChannelInfo() (available bool, channel Channel) {
	if c.videosInitialComplete || c.streamsInitialComplete {
		channel = c.channel
		available = true
	} else {
		available = false
	}

	return
}

type YoutubeImage struct {
	Url    string `rjson:"url"`
	Width  int    `rjson:"width"`
	Height int    `rjson:"height"`
}

type channelInitialAccount struct {
	Subscribers     string   `rjson:"header.c4TabbedHeaderRenderer.subscriberCountText.simpleText"`
	ChannelID       string   `rjson:"metadata.channelMetadataRenderer.externalId"`
	NewChannelID    string   `rjson:"header.c4TabbedHeaderRenderer.channelHandleText.runs[0].text"`
	Username        string   `rjson:"metadata.channelMetadataRenderer.title"`
	Description     string   `rjson:"metadata.channelMetadataRenderer.description"`
	RawVideosAmount string   `rjson:"header.c4TabbedHeaderRenderer.videosCountText.runs[0].text"`
	Badges          []string `rjson:"header.c4TabbedHeaderRenderer.badges[].metadataBadgeRenderer.tooltip"`

	Avatars []YoutubeImage `rjson:"header.c4TabbedHeaderRenderer.avatar.thumbnails"`
	Banners []YoutubeImage `rjson:"header.c4TabbedHeaderRenderer.banner.thumbnails"`
}

// videoRenderer json type
type videoRenderer struct {
	VideoID string `rjson:"videoId"`
	Title   string `rjson:"title.runs[0].text"`
	Length  string `rjson:"lengthText.simpleText"`
	Views   string `rjson:"viewCountText.simpleText"`
	Viewers string `rjson:"viewCountText.runs[0].text"`
	Date    string `rjson:"publishedTimeText.simpleText"`
}

func (video videoRenderer) ToVideo(channel *Channel) (v Video, err error) {
	date, wasLive := strings.CutPrefix(video.Date, "Streamed ")

	var views int
	if video.Views != "" {
		views, err = strconv.Atoi(fixUnit(strings.ReplaceAll(strings.TrimSuffix(video.Views, " views"), ",", "")))
		if err != nil {
			return
		}
	}

	var viewers int
	if video.Viewers != "" {
		viewers, err = strconv.Atoi(fixUnit(strings.ReplaceAll(strings.TrimSuffix(video.Viewers, " watching"), ",", "")))
		if err != nil {
			return
		}
	}

	v = Video{
		VideoID:                video.VideoID,
		Title:                  video.Title,
		Length:                 video.Length,
		Views:                  views,
		Viewers:                viewers,
		Date:                   date,
		ChannelID:              channel.ChannelID,
		NewChannelID:           channel.NewChannelID,
		WasLive:                wasLive,
		IsLive:                 len(video.Viewers) > 0,
		AuthorIsVerified:       channel.IsVerified,
		AuthorIsVerifiedArtist: channel.IsVerifiedArtist,
	}
	return
}

type channelVideosInitialOutput struct {
	Channel                 channelInitialAccount `rjson:"."`
	Videos                  []videoRenderer       `rjson:"contents.twoColumnBrowseResultsRenderer.tabs[1].tabRenderer.content.richGridRenderer.contents[].richItemRenderer.content.videoRenderer"`
	VideosContinuationToken string                `rjson:"contents.twoColumnBrowseResultsRenderer.tabs[1].tabRenderer.content.richGridRenderer.contents[-].continuationItemRenderer.continuationEndpoint.continuationCommand.token"`
}
type channelStreamsInitialOutput struct {
	Channel channelInitialAccount `rjson:"."`
	Tabs    []struct {
		Title  string          `rjson:"title"`
		Token  string          `rjson:"content.richGridRenderer.contents[-].continuationItemRenderer.continuationEndpoint.continuationCommand.token"`
		Videos []videoRenderer `rjson:"content.richGridRenderer.contents[].richItemRenderer.content.videoRenderer"`
	} `rjson:"contents.twoColumnBrowseResultsRenderer.tabs[].tabRenderer"`
}

type channelContinueOutput struct {
	Videos        []videoRenderer `rjson:"onResponseReceivedActions[0].appendContinuationItemsAction.continuationItems[].richItemRenderer.content.videoRenderer"`
	ContinueToken string          `rjson:"onResponseReceivedActions[0].appendContinuationItemsAction.continuationItems[-]continuationItemRenderer.continuationEndpoint.continuationCommand.token"`
}

func genericChannelInitial(initialComplete *bool, url string, channel *Channel, continueInputJson *[]byte, outputGeneric func(rawJson []byte) (rawChannel channelInitialAccount, rawVideos []videoRenderer, rawToken string, err error)) (videos []Video, err error) {
	var rawJson string
	rawJson, err = extractInitialData(url)
	if err != nil {
		return
	}

	rawChannel, rawVideos, rawToken, err := outputGeneric([]byte(rawJson))
	if err != nil {
		return
	}

	subscribers, unit, err := humanize.ParseSI(fixUnit(strings.TrimSuffix(rawChannel.Subscribers, " subscribers")))
	if err != nil {
		return
	} else if unit != "" {
		log.Printf("WARNING: possibly wrong number for channel subscribers count: %f%s\n", subscribers, unit)
	}

	var videosAmount float64
	if rawChannel.RawVideosAmount == "No videos" {
		videosAmount = 0
	} else {
		videosAmount, unit, err = humanize.ParseSI(fixUnit(rawChannel.RawVideosAmount))
		if err != nil {
			return
		} else if unit != "" {
			log.Printf("WARNING: possibly wrong number for channel videos amount: %f%s\n", videosAmount, unit)
		}
	}

	*channel = Channel{
		Subscribers:  int(subscribers),
		ChannelID:    rawChannel.ChannelID,
		NewChannelID: rawChannel.NewChannelID,
		Username:     rawChannel.Username,
		Description:  rawChannel.Description,
		VideosAmount: int(videosAmount),
		Avatars:      rawChannel.Avatars,
		Banners:      rawChannel.Banners,
	}

	for _, badge := range rawChannel.Badges {
		switch badge {
		case BadgeChannelVerified:
			channel.IsVerified = true
		case BadgeChannelVerifiedArtistChannel:
			channel.IsVerifiedArtist = true
		}
	}

	*continueInputJson, err = continueInput{Continuation: rawToken}.FillGenericInfo().Construct()
	if err != nil {
		return
	}

	for _, video := range rawVideos {
		if video.VideoID != "" {
			if v, err := video.ToVideo(channel); err != nil {
				log.Println("WARNING error while converting video:", err)
			} else {
				videos = append(videos, v)
			}
		}
	}

	*initialComplete = true
	return
}

func genericChannelPage(channel *Channel, continueInputJson *[]byte, outputGeneric func(rawJson []byte) (rawToken string, rawVideos []videoRenderer, err error)) (videos []Video, err error) {
	var resp *http.Response
	resp, err = http.Post("https://www.youtube.com/youtubei/v1/browse", "application/json", bytes.NewReader(*continueInputJson))
	if err != nil {
		return
	}
	*continueInputJson = []byte{}

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	rawToken, rawVideos, err := outputGeneric(body)
	if err != nil {
		return
	}

	*continueInputJson, err = continueInput{Continuation: rawToken}.FillGenericInfo().Construct()
	if err != nil {
		return
	}

	for _, video := range rawVideos {
		if video.VideoID != "" {
			if v, err := video.ToVideo(channel); err != nil {
				log.Println("WARNING error while converting video:", err)
			} else {
				videos = append(videos, v)
			}
		}
	}

	return
}

// NextVideosPage scrapes pages of the `/videos` endpoint on channel page
func (c *ChannelScraper) NextVideosPage() (videos []Video, err error) {
	if !c.videosInitialComplete {
		return genericChannelInitial(&c.videosInitialComplete, c.videosUrl, &c.channel, &c.videosContinueInputJson, func(rawJson []byte) (rawChannel channelInitialAccount, rawVideos []videoRenderer, rawToken string, err error) {
			debugFileOutput(rawJson, "channel_videos_initial.json")

			var output channelVideosInitialOutput
			if err = rjson.Unmarshal(rawJson, &output); err != nil {
				if errors.Is(err, rjson.ErrCantFindField) {
					if Debug {
						log.Println("WARNING:", err)
					}
					err = nil
				}
				return
			}

			rawChannel = output.Channel
			rawVideos = output.Videos
			rawToken = output.VideosContinuationToken

			return
		})
	} else {
		return genericChannelPage(&c.channel, &c.videosContinueInputJson, func(rawJson []byte) (rawToken string, rawVideos []videoRenderer, err error) {
			debugFileOutput(rawJson, "channel_videos.json")

			var output channelContinueOutput
			if err = rjson.Unmarshal(rawJson, &output); err != nil {
				if errors.Is(err, rjson.ErrCantFindField) {
					err = nil
				}
				return
			}

			rawToken = output.ContinueToken
			rawVideos = output.Videos
			return
		})
	}
}

// NextStreamsPage scrapes pages of the `/streams` endpoint on channel page
func (c *ChannelScraper) NextStreamsPage() (videos []Video, err error) {
	if !c.streamsInitialComplete {
		videos, err = genericChannelInitial(&c.streamsInitialComplete, c.streamsUrl, &c.channel, &c.streamsContinueInputJson, func(rawJson []byte) (rawChannel channelInitialAccount, rawVideos []videoRenderer, rawToken string, err error) {
			debugFileOutput(rawJson, "channel_streams_initial.json")

			var output channelStreamsInitialOutput
			if err = rjson.Unmarshal(rawJson, &output); err != nil {
				if errors.Is(err, rjson.ErrCantFindField) {
					if Debug {
						log.Println("WARNING:", err)
					}
					err = nil
				}
				return
			}

			rawChannel = output.Channel

			for _, tab := range output.Tabs {
				if tab.Title == "Live" {
					rawVideos = tab.Videos
					rawToken = tab.Token
				}
			}

			return
		})
		if err != nil {
			return
		}

		// fix for pagination api sometimes not working
		if len(videos) == 0 {
			return c.NextStreamsPage()
		} else {
			return
		}
	} else {
		// fix for pagination api sometimes not working
		for i := 0; i < 3; i++ {
			videos, err = genericChannelPage(&c.channel, &c.streamsContinueInputJson, func(rawJson []byte) (rawToken string, rawVideos []videoRenderer, err error) {
				debugFileOutput(rawJson, "channel_streams.json")

				var output channelContinueOutput
				if err = rjson.Unmarshal(rawJson, &output); err != nil {
					if errors.Is(errors.Unwrap(err), rjson.ErrCantFindField) {
						err = nil
					}
					return
				}

				fmt.Println(rawToken)
				rawToken = output.ContinueToken
				rawVideos = output.Videos

				return
			})

			if err != nil {
				return
			}

			if len(videos) > 0 {
				break
			}
		}

		return
	}
}
