package main

import (
	"flag"
	"log"
	"os"
	"path"

	"github.com/song940/gan"
)

func main() {
	var port string
	home, _ := os.UserHomeDir()
	root := path.Join(home, "Projects")
	flag.StringVar(&root, "root", root, "repos root dir")
	flag.StringVar(&port, "port", "3456", "listen port")
	flag.Parse()

	sc := NewSmithy(root)
	sc.LoadAllRepositories()
	app := gan.New()

	t, _ := loadTemplates()
	app.SetTemplate(t)
	app.GET("/", sc.IndexView)
	app.GET("/new", sc.NewProjectView)
	app.POST("/new", sc.NewProject)
	app.GET("/reload", sc.Reload)
	app.GET("/:repo", sc.RepoView)

	app.GET("/:repo/refs", sc.RefsView)
	app.GET("/:repo/log/:ref?", sc.LogView)
	app.GET("/:repo/patch/:hash", sc.PatchView)
	app.GET("/:repo/commit/:hash", sc.CommitView)
	app.GET("/:repo/tree/:ref?/:path*", sc.TreeView)

	app.GET("/:repo/info/refs", sc.getInfoRefs)
	app.POST("/:repo/git-upload-pack", sc.uploadPack)
	app.POST("/:repo/git-receive-pack", sc.receivePack)

	err := app.Run(":" + port)
	if err != nil {
		log.Fatal("ERROR:", err)
	}
}
