// Copyright © 2014 Steve Francia <spf@spf13.com>.
//
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package commands

import (
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"labix.org/v2/mgo/bson"

	"github.com/GeertJohan/go.rice"
	"github.com/gin-gonic/gin"
	"github.com/pilu/fresh/runner/runnerutils"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const pLimit = 10

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Server for feeds",
	Long:  `Dagobah will serve all feeds listed in the config file.`,
	Run:   serverRun,
}

func init() {
	serverCmd.Flags().Int("port", 1138, "Port to run Dagobah server on")
	viper.BindPFlag("port", serverCmd.Flags().Lookup("port"))
}

func serverRun(cmd *cobra.Command, args []string) {
	Server()
}

func Server() {
	port := viper.GetString("port")

	r := gin.Default()

	if os.Getenv("DEV") != "" {
		r.Use(RunnerMiddleware())
	}

	templates := loadTemplates("home.html", "channels.html", "items.html", "main.html")
	r.HTMLTemplates = templates

	r.GET("/ping", func(c *gin.Context) {
		c.String(200, "pong")
	})

	r.GET("/", homeRoute)
	r.GET("/post/*key", postRoute)
	r.GET("/search/*query", searchRoute)
	r.GET("/static/*filepath", staticServe)
	r.GET("/channel/*key", channelRoute)
	fmt.Println("Running on port:", port)
	r.Run(":" + port)
}

func staticServe(c *gin.Context) {
	static, err := rice.FindBox("static")
	if err != nil {
		log.Fatal(err)
	}
	original := c.Req.URL.Path
	c.Req.URL.Path = c.Params.ByName("filepath")
	http.FileServer(static.HTTPBox()).ServeHTTP(c.Writer, c.Req)
	c.Req.URL.Path = original
}

func RunnerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if runnerutils.HasErrors() {
			runnerutils.RenderError(c.Writer)
			c.Abort(500)
		}
	}
}

func loadTemplates(list ...string) *template.Template {
	templateBox, err := rice.FindBox("templates")
	if err != nil {
		log.Fatal(err)
	}

	templates := template.New("")

	for _, x := range list {
		templateString, err := templateBox.String(x)
		if err != nil {
			log.Fatal(err)
		}

		// get file contents as string
		_, err = templates.New(x).Parse(templateString)
		if err != nil {
			log.Fatal(err)
		}
	}

	funcMap := template.FuncMap{
		"html":  ProperHtml,
		"title": func(a string) string { return strings.Title(a) },
	}

	templates.Funcs(funcMap)

	return templates
}

func ProperHtml(text string) template.HTML {
	if strings.Contains(text, "content:encoded>") || strings.Contains(text, "content/:encoded>") {
		text = html.UnescapeString(text)
	}
	return template.HTML(html.UnescapeString(template.HTMLEscapeString(text)))
}

func postRoute(c *gin.Context) {

	key := c.Params.ByName("key")

	if len(key) < 2 {
		four04(c, "Invalid Channel")
		return
	}

	key = key[1:]

	// TODO Need to find posts before and after this... not just the first ones
	var posts []Itm
	results := Items().Find(bson.M{}).Sort("-date").Limit(pLimit)
	results.All(&posts)

	var post Itm
	Items().Find(bson.M{"key": key}).Sort("-date").One(&post)

	channels := AllChannels()

	obj := gin.H{"title": post.Title, "post": post, "items": posts, "channels": channels}

	if strings.ToLower(c.Req.Header.Get("X-Requested-With")) == "xmlhttprequest" {
		c.HTML(200, "main.html", obj)
	} else {
		c.HTML(200, "home.html", obj)
	}
}

func Offset(c *gin.Context) int {
	curPage := cast.ToInt(c.Req.FormValue("p")) - 1
	if curPage < 1 {
		return 0
	}
	return pLimit * curPage
}

func four04(c *gin.Context, message string) {
	c.HTML(404, "home.html", gin.H{"message": message, "title": viper.GetString("title")})
}

func homeRoute(c *gin.Context) {

	channels := AllChannels()

	var posts []Itm
	results := Items().Find(bson.M{}).Skip(Offset(c)).Sort("-date").Limit(pLimit)
	results.All(&posts)

	if len(posts) == 0 {
		four04(c, "No Articles")
		return
	}

	obj := gin.H{"title": viper.GetString("title"), "items": posts, "post": posts[0], "channels": channels}

	if strings.ToLower(c.Req.Header.Get("X-Requested-With")) == "xmlhttprequest" {
		c.HTML(200, "items.html", obj)
	} else {
		c.HTML(200, "home.html", obj)
	}
}

func searchRoute(c *gin.Context) {
	q := c.Params.ByName("query")
	if len(q) < 2 {
		four04(c, "Query is too short. Please try a longer query.")
		return
	}

	q = q[1:]

	channels := AllChannels()

	var posts []Itm
	// TODO need to send a PR to Gustavo with support for textscore sorting
	results := Items().Find(bson.M{"$text": bson.M{"$search": q}}).Skip(Offset(c)).Limit(pLimit)

	results.All(&posts)

	if len(posts) == 0 {
		four04(c, "No Articles for query '"+q+"'")
		return
	}

	obj := gin.H{"title": q, "header": q, "items": posts, "post": posts[0], "channels": channels}

	if strings.ToLower(c.Req.Header.Get("X-Requested-With")) == "xmlhttprequest" {
		c.HTML(200, "items.html", obj)
	} else {
		c.HTML(200, "home.html", obj)
	}
}

func channelRoute(c *gin.Context) {
	key := c.Params.ByName("key")
	if len(key) < 2 {
		four04(c, "Channel Not Found")
		return
	}

	key = key[1:]

	var posts []Itm
	results := Items().Find(bson.M{"channelkey": key}).Skip(Offset(c)).Sort("-date").Limit(pLimit)
	results.All(&posts)

	if len(posts) == 0 {
		four04(c, "No Articles")
		return
	}

	channels := AllChannels()

	var currentChannel Chnl
	err := Channels().Find(bson.M{"key": key}).One(&currentChannel)
	if err != nil {
		if string(err.Error()) == "not found" {
			four04(c, "Channel not found")
			return
		} else {
			fmt.Println(err)
		}
	}

	obj := gin.H{"title": currentChannel.Title, "header": currentChannel.Title, "post": posts[0], "items": posts, "channels": channels}

	if strings.ToLower(c.Req.Header.Get("X-Requested-With")) == "xmlhttprequest" {
		c.HTML(200, "items.html", obj)
	} else {
		c.HTML(200, "home.html", obj)
	}
}
