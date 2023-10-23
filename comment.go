package scraper

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/ayes-web/rjson"
	"github.com/dustin/go-humanize"
)

type Comment struct {
	NewChannelID  string
	CommentID     string
	Content       string
	PublishedTime string // "5 days ago"
	Likes         int
	PinnedBy      string // "Pinned by Username"
	IsPinned      bool
	IsHearted     bool
	WasEdited     bool // if the comment was edited
	RepliesAmount int

	repliesToken             string
	repliesContinueInputJson []byte
}

// HasSubComments returns if the comment has any replies
func (c *Comment) HasSubComments() bool {
	return c.repliesToken != ""
}

// youtube json commentRenderer type
type commentRenderer struct {
	NewChannelID  string   `rjson:"authorText.simpleText"`
	CommentID     string   `rjson:"commentId"`
	Content       []string `rjson:"contentText.runs[].text"`
	PublishedTime string   `rjson:"publishedTimeText.runs[0].text"` // ends with "(edited)" if the comment has been edited
	LikeAmount    string   `rjson:"voteCount.simpleText"`           // 3K
	Pinned        []string `rjson:"pinnedCommentBadge.pinnedCommentBadgeRenderer.label.runs[].text"`
	IsHearted     bool     `rjson:"actionButtons.commentActionButtonsRenderer.creatorHeart.creatorHeartRenderer.isHearted"`
}

type subCommentsContinueOutput struct {
	Comments      []commentRenderer `rjson:"onResponseReceivedEndpoints[0]appendContinuationItemsAction.continuationItems[].commentRenderer"`
	ContinueToken string            `rjson:"onResponseReceivedEndpoints[0]appendContinuationItemsAction.continuationItems[-].continuationItemRenderer.button.buttonRenderer.command.continuationCommand.token"`
}

// NextSubCommentPage returns comment replies in chunks. Check with HasSubComments if there are replies
func (c *Comment) NextSubCommentPage() (comments []Comment, err error) {
	var resp *http.Response
	resp, err = http.Post("https://www.youtube.com/youtubei/v1/next", "application/json", bytes.NewReader(c.repliesContinueInputJson))
	if err != nil {
		return
	}
	c.repliesContinueInputJson = []byte{}

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	debugFileOutput(body, "subcomment_%s.json", c.repliesToken)

	var output subCommentsContinueOutput
	if err = rjson.Unmarshal(body, &output); err != nil {
		if errors.Is(err, rjson.ErrCantFindField) {
			if Debug {
				log.Println("WARNING:", err)
			}
			err = nil
		}
		return
	}

	c.repliesToken = output.ContinueToken
	c.repliesContinueInputJson, err = continueInput{Continuation: c.repliesToken}.FillGenericInfo().Construct()
	if err != nil {
		return
	}

	for _, comment := range output.Comments {
		if comment.CommentID != "" {
			publishedTime, wasEdited := strings.CutSuffix(comment.PublishedTime, " (edited)")

			var likes float64
			if comment.LikeAmount != "" {
				var unit string
				likes, unit, err = humanize.ParseSI(fixUnit(comment.LikeAmount))
				if err != nil {
					log.Println("WARNING:", err)
					continue
				} else if unit != "" {
					log.Printf("WARNING: possibly wrong number for likes: %f%s\n", likes, unit)
				}
			}

			comments = append(comments, Comment{
				NewChannelID:  comment.NewChannelID,
				CommentID:     comment.CommentID,
				Content:       strings.Join(comment.Content, ""),
				PublishedTime: publishedTime,
				WasEdited:     wasEdited,
				Likes:         int(likes),
				PinnedBy:      strings.Join(comment.Pinned, ""),
				IsPinned:      len(comment.Pinned) > 0,
				IsHearted:     comment.IsHearted,
			})
		}
	}

	return
}

// commentThreadRenderer json type
type commentContinueOutputComment struct {
	Comment       commentRenderer `rjson:"comment.commentRenderer"`
	RepliesAmount string          `rjson:"replies.commentRepliesRenderer.viewReplies.buttonRenderer.text.runs[0].text"`
	RepliesToken  string          `rjson:"replies.commentRepliesRenderer.contents[-].continuationItemRenderer.continuationEndpoint.continuationCommand.token"`
}

type commentsContinueOutputInitial struct {
	Comments      []commentContinueOutputComment `rjson:"onResponseReceivedEndpoints[1]reloadContinuationItemsCommand.continuationItems[].commentThreadRenderer"`
	ContinueToken string                         `rjson:"onResponseReceivedEndpoints[1]reloadContinuationItemsCommand.continuationItems[-]continuationItemRenderer.continuationEndpoint.continuationCommand.token"`
}
type commentsContinueOutput struct {
	Comments      []commentContinueOutputComment `rjson:"onResponseReceivedEndpoints[0]appendContinuationItemsAction.continuationItems[].commentThreadRenderer"`
	ContinueToken string                         `rjson:"onResponseReceivedEndpoints[0]appendContinuationItemsAction.continuationItems[-]continuationItemRenderer.continuationEndpoint.continuationCommand.token"`
}

func genericNextCommentsPage(input *continueInput, continueInputJson *[]byte, outputGeneric func(rawJson []byte) (rawToken string, rawComments []commentContinueOutputComment, err error)) (comments []Comment, err error) {
	var resp *http.Response
	resp, err = http.Post("https://www.youtube.com/youtubei/v1/next", "application/json", bytes.NewReader(*continueInputJson))
	if err != nil {
		return
	}
	*continueInputJson = []byte{}

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	rawToken, rawComments, err := outputGeneric(body)
	if err != nil {
		return
	}

	*input = continueInput{Continuation: rawToken}.FillGenericInfo()
	*continueInputJson, err = input.Construct()
	if err != nil {
		return
	}

	for _, comment := range rawComments {
		if comment.Comment.CommentID == "" {
			continue
		}

		var (
			repliesToken             string
			repliesContinueInputJson []byte
		)
		if comment.RepliesToken != "" {
			repliesToken = comment.RepliesToken
			repliesContinueInputJson, err = continueInput{Continuation: repliesToken}.FillGenericInfo().Construct()
			if err != nil {
				return
			}
		}

		publishedTime, wasEdited := strings.CutSuffix(comment.Comment.PublishedTime, " (edited)")

		var likes float64
		if comment.Comment.LikeAmount != "" {
			var unit string
			likes, unit, err = humanize.ParseSI(fixUnit(comment.Comment.LikeAmount))
			if err != nil {
				log.Println("WARNING:", err)
				continue
			} else if unit != "" {
				log.Printf("WARNING: possibly wrong number for likes: %f%s\n", likes, unit)
			}
		}

		var repliesAmount int
		if comment.RepliesAmount != "" {
			repliesAmount, err = strconv.Atoi(fixUnit(strings.ReplaceAll(strings.TrimSuffix(strings.TrimSuffix(comment.RepliesAmount, " replies"), " reply"), ",", "")))
			if err != nil {
				log.Println("WARNING:", err)
				continue
			}
		}

		comments = append(comments, Comment{
			NewChannelID:             comment.Comment.NewChannelID,
			CommentID:                comment.Comment.CommentID,
			Content:                  strings.Join(comment.Comment.Content, ""),
			PublishedTime:            publishedTime,
			WasEdited:                wasEdited,
			Likes:                    int(likes),
			PinnedBy:                 strings.Join(comment.Comment.Pinned, ""),
			IsPinned:                 len(comment.Comment.Pinned) > 0,
			IsHearted:                comment.Comment.IsHearted,
			RepliesAmount:            repliesAmount,
			repliesToken:             repliesToken,
			repliesContinueInputJson: repliesContinueInputJson,
		})
	}

	return
}

// NextNewestCommentsPage returns comments in chunks sorted by newest
func (v *VideoScraper) NextNewestCommentsPage() (comments []Comment, err error) {
	return genericNextCommentsPage(&v.commentsNewestContinueInput, &v.commentsNewestContinueInputJson, func(rawJson []byte) (rawToken string, rawComments []commentContinueOutputComment, err error) {
		if !v.commentsNewestPassedInitial {
			debugFileOutput(rawJson, "comment_newest_initial_%s.json", v.commentsNewestContinueInput.Continuation)

			var output commentsContinueOutputInitial
			if err = rjson.Unmarshal(rawJson, &output); err != nil {
				if errors.Is(err, rjson.ErrCantFindField) {
					if Debug {
						log.Println("WARNING:", err)
					}
					err = nil
				}
				return
			}

			rawToken = output.ContinueToken
			rawComments = output.Comments
			v.commentsNewestPassedInitial = true
		} else {
			debugFileOutput(rawJson, "comment_newest_%s.json", v.commentsNewestContinueInput.Continuation)

			var output commentsContinueOutput
			if err = rjson.Unmarshal(rawJson, &output); err != nil {
				if errors.Is(err, errors.Unwrap(err)) {
					if Debug {
						log.Println("WARNING:", err)
					}
					err = nil
				}
				return
			}

			rawToken = output.ContinueToken
			rawComments = output.Comments
		}

		return
	})
}

// NextTopCommentsPage returns comments in chunks sorted by most popular
func (v *VideoScraper) NextTopCommentsPage() (comments []Comment, err error) {
	return genericNextCommentsPage(&v.commentsTopContinueInput, &v.commentsTopContinueInputJson, func(rawJson []byte) (rawToken string, rawComments []commentContinueOutputComment, err error) {
		debugFileOutput(rawJson, "comment_top_%s.json", v.commentsTopContinueInput.Continuation)

		if !v.commentsTopPassedInitial {
			var output commentsContinueOutputInitial
			if err = rjson.Unmarshal(rawJson, &output); err != nil {
				if errors.Is(err, errors.Unwrap(err)) {
					if Debug {
						log.Println("WARNING:", err)
					}
					err = nil
				}
				return
			}

			rawToken = output.ContinueToken
			rawComments = output.Comments
			v.commentsTopPassedInitial = true
		} else {
			var output commentsContinueOutput
			if err = rjson.Unmarshal(rawJson, &output); err != nil {
				if errors.Is(err, errors.Unwrap(err)) {
					if Debug {
						log.Println("WARNING:", err)
					}
					err = nil
				}
				return
			}

			rawToken = output.ContinueToken
			rawComments = output.Comments
		}

		return
	})
}
