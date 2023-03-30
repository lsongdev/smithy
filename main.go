// smithy --- the git forge
// Copyright (C) 2020   Honza Pokorny <honza@pokorny.ca>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"

	"github.com/gin-gonic/gin"
)

func main() {
	home, _ := os.UserHomeDir()
	root := path.Join(home, "Projects")
	flag.StringVar(&root, "root", root, "repos root dir")
	flag.Parse()

	sc := NewSmithy()
	sc.LoadAllRepositories(root)
	templ, err := loadTemplates()
	if err != nil {
		log.Fatal("Failed to load templates:", err)
		return
	}
	app := gin.Default()
	app.SetHTMLTemplate(templ)
	app.Use(AddConfigMiddleware(sc))
	routes := CompileRoutes()
	app.Any("*path", func(ctx *gin.Context) {
		Dispatch(ctx, routes, http.FileServer(http.FS(staticfiles)))
	})
	err = app.Run(":" + fmt.Sprint(sc.Port))
	if err != nil {
		log.Fatal("ERROR:", err, sc.Port)
	}
}
