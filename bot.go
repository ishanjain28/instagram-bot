package main

import (
	"os"
	tbot "github.com/go-telegram-bot-api/telegram-bot-api"
	"net/http"
	scraper2 "github.com/cardigann/go-cloudflare-scraper"
	"golang.org/x/net/html"
	"strings"
	"io/ioutil"
	"net/url"
)

var (
	TOKEN  = ""
	GO_ENV = ""
	PORT   = ""
)

type InstagramResponse struct {
	Username string
	Realname string
	image    string
}

func main() {

	Info.Println("Starting Bot...")

	GO_ENV = os.Getenv("GO_ENV")
	if GO_ENV == "" {
		Warn.Println("$GO_ENV not set")
		GO_ENV = "development"
	}

	Info.Println("$GO_ENV=" + GO_ENV)

	if GO_ENV != "development" {
		PORT = os.Getenv("PORT")
		if PORT == "" {
			Error.Fatalln("$PORT not set")
		}
	}

	TOKEN = os.Getenv("TOKEN")
	if TOKEN == "" {
		Error.Fatalln("$TOKEN not set")
	}

	bot, err := tbot.NewBotAPI(TOKEN)
	if err != nil {
		Error.Fatalln("Error in starting bot", err.Error())
	}
	//if GO_ENV == "development" {
	bot.Debug = false
	//}

	Info.Printf("Authorized on account %s\n", bot.Self.UserName)

	updates := fetchUpdates(bot)

	for update := range updates {
		if update.Message == nil {
			msg := tbot.NewMessage(update.Message.Chat.ID, "Sorry, I am not sure what you mean, Type /help to get help")
			bot.Send(msg)
			continue
		}
		handleUpdates(bot, update)
	}
}

func fetchUpdates(bot *tbot.BotAPI) tbot.UpdatesChannel {
	if GO_ENV == "development" {
		//Use polling, because testing on local machine

		//Remove webhook
		bot.RemoveWebhook()

		Info.Println("Using Polling Method to fetch updates")
		u := tbot.NewUpdate(0)
		u.Timeout = 60
		updates, err := bot.GetUpdatesChan(u)
		if err != nil {
			Warn.Println("Problem in fetching updates", err.Error())
		}

		return updates

	} else {
		//	USe Webhooks, because deploying on heroku
		Info.Println("Setting webhooks to fetch updates")
		_, err := bot.SetWebhook(tbot.NewWebhookWithCert("https://dry-hamlet-60060.herokuapp.com:443"+"/"+bot.Token, "cert.pem"))
		if err != nil {
			Error.Fatalln("Problem in setting webhook", err.Error())
		}

		updates := bot.ListenForWebhook("/" + bot.Token)

		Info.Println("Starting HTTPS Server")
		go http.ListenAndServeTLS(":"+PORT, "cert.pem", "key.pem", nil)

		return updates
	}
}

func handleUpdates(bot *tbot.BotAPI, u tbot.Update) {

	if u.Message.IsCommand() {
		switch u.Message.Text {
		case "/start", "/help":
			msg := tbot.NewMessage(u.Message.Chat.ID, "Give me an Instagram User, And I'll give you their Profile Picture")
			msg.ReplyToMessageID = u.Message.MessageID
			bot.Send(msg)

		default:
			msg := tbot.NewMessage(u.Message.Chat.ID, "Invalid Command")
			msg.ReplyToMessageID = u.Message.MessageID
			bot.Send(msg)
		}
		return
	}

	if u.Message.Text != "" {

		i, err := fetchInstagramPhoto(u.Message.Text)
		if err != nil {
			Warn.Println("Error in fetching Profile Picture", err.Error())

			msg := tbot.NewMessage(u.Message.Chat.ID, "Error in fetching User's Profile Picture")
			msg.ReplyToMessageID = u.Message.MessageID
			bot.Send(msg)
			return
		}

		if i.Username == "" && i.Realname == "" && i.image == "" {
			//	No such user
			msg := tbot.NewMessage(u.Message.Chat.ID, "Invalid User ID, Enter Valid User ID")
			msg.ReplyToMessageID = u.Message.MessageID
			bot.Send(msg)
			return
		}

		Info.Printf("Serving %s (@%s) Profile Picture\n", i.Realname, i.Username)
		//Info.Printf("%s's Image: %s\n", i.Username, i.image)
		imgBytes, err := downloadImage(i.image)

		if err != nil {
			Warn.Println("Error in downloading Image", err.Error())
			msg := tbot.NewMessage(u.Message.Chat.ID, "Error in downloading Image, Please retry")
			msg.ReplyToMessageID = u.Message.MessageID
			bot.Send(msg)
			return
		}

		msg := tbot.NewPhotoUpload(u.Message.Chat.ID, tbot.FileBytes{"image", imgBytes})
		msg.ReplyToMessageID = u.Message.MessageID

		bot.Send(msg)

	}
}
func fetchInstagramPhoto(u string) (*InstagramResponse, error) {

	scraper, err := scraper2.NewTransport(http.DefaultTransport)
	if err != nil {
		return nil, err
	}

	c := http.Client{Transport: scraper}
	j := parseInput(u)

	Info.Println(j)
	res, err := c.Get(j)

	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	h, err := html.Parse(res.Body)
	if err != nil {
		Warn.Println("Problem in parsing instagram page", err.Error())

		return nil, err
	}

	var f func(node *html.Node)

	i := &InstagramResponse{}
	i.Username = u

	f = func(n *html.Node) {

		if n.Type == html.ElementNode && n.Data == "meta" {

			find(n.Attr, i)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(h)

	return i, nil
}

func parseInput(u string) string {
	Info.Println(u)
	j, err := url.ParseRequestURI(u)
	if err != nil {
		Warn.Println("Not a url")
		//	It's a username
		return "https://instagram.com/" + u
	}

	if j.Scheme == "https" && j.Host == "instagram.com" {
		Warn.Println("URL", "https://instagram.com/"+j.RawPath, " ", j.Path)
		return "https://instagram.com/" + j.Path
	}
	return u
}

func find(attr []html.Attribute, insta *InstagramResponse) {

	for _, a := range attr {
		if a.Val == "og:image" {
			for _, v := range attr {
				if v.Key == "content" {

					img := strings.Replace(v.Val, "s150x150", "s1080x1080", 1)

					insta.image = img

				}
			}
		}

		if a.Val == "og:title" {
			for _, v := range attr {
				if v.Key == "content" {

					a := strings.Split(v.Val, " (@")

					insta.Realname = a[0]

				}
			}
		}
	}
}

func downloadImage(u string) ([]byte, error) {
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
