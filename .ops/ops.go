package main

import (
	"os"

	"labs.lesiw.io/ops/github"
	"labs.lesiw.io/ops/goapp"
	k8sapp "labs.lesiw.io/ops/k8s/goapp"
	"lesiw.io/ops"
)

type k8sOps = k8sapp.Ops
type ghOps = github.Ops

type Ops struct {
	k8sOps
	ghOps
}

var secrets = map[string]string{
	"DISCORD_TOKEN": "discord/bot",
}

func main() {
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "build")
	}
	github.Repo = "lesiwlabs/discord"
	github.Secrets = secrets
	goapp.Name = "discord"
	o := Ops{}
	o.EnvSecrets = secrets
	o.Postgres = true
	ops.Handle(o)
}
