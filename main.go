/*
Copyright © 2023 Kevin Gong <aoxianglele@icloud.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"fmt"

	"github.com/KevinGong2013/apkgo/cmd"
)

// write form ldflags
// go build -ldflags='-s -w -X main.version=1.0.0'
var (
	version = "dev"
	date    = "unknown"
	goOS    = "unknown"
	goArch  = "unknown"
)

func main() {
	fmt.Printf("apkgo %s %s/%s built at %s\n\n", version, goOS, goArch, date)
	cmd.Execute()
}
