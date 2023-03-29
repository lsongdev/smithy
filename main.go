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
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	config := NewConfig()
	app := gin.Default()
	err := config.LoadAllRepositories()
	templ, err := loadTemplates(config)
	if err != nil {
		fmt.Println("Failed to load templates:", err)
		return
	}
	app.SetHTMLTemplate(templ)
	app.Use(AddConfigMiddleware(config))
	routes := CompileRoutes()
	app.Any("*path", func(ctx *gin.Context) {
		Dispatch(ctx, routes, http.FileServer(http.FS(staticfiles)))
	})

	err = app.Run(":" + fmt.Sprint(config.Port))

	if err != nil {
		fmt.Println("ERROR:", err, config.Port)
	}
	return
}
