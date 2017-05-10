package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/Masterminds/glide/action"
	"github.com/Masterminds/glide/cache"
	"github.com/Masterminds/glide/cfg"
	"github.com/Masterminds/glide/msg"
	gpath "github.com/Masterminds/glide/path"
	"github.com/Masterminds/glide/repo"
	"github.com/urfave/cli"
)

const (
	BackupSuffix = ".hyperglide.hgbak"
)

var ErrAlreadyReportedError = fmt.Errorf("An error occurred")

func LoadGlideYaml(path string) (*cfg.Config, error) {
	yml, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return cfg.ConfigFromYaml(yml)
}

func LoadGlideLock(path string) (*cfg.Lockfile, error) {
	return cfg.ReadLockFile(path)
}

type LockMapper func(*cfg.Lock) *cfg.Dependency

func doUpdate(gypath, glpath, home string, mapper LockMapper) error {
	gy, err := LoadGlideYaml(gypath)
	if err != nil {
		return err
	}

	gl, err := LoadGlideLock(glpath)
	if err != nil {
		return err
	}

	hgy := &cfg.Config{
		Name: gy.Name,
	}

	imports := map[string]*cfg.Dependency{}

	for _, lock := range gl.Imports {
		dep := mapper(lock)
		imports[dep.Name] = dep
		hgy.AddImport(dep)
	}

	for _, thisImport := range gy.Imports {
		existing, ok := imports[thisImport.Name]
		if !ok {
			err = hgy.AddImport(thisImport)
			if err != nil {
				return err
			}
		}

		existing.Reference = thisImport.Reference
		existing.VcsType = thisImport.VcsType
		existing.Repository = thisImport.Repository
	}

	err = os.Rename(gypath, gypath+BackupSuffix)
	if err != nil {
		return err
	}

	defer func() {
		err = os.Rename(gypath+BackupSuffix, gypath)
		if err != nil {
			msg.Err("error restoring %s: %s", gypath, err.Error())
		}
	}()

	hgyData, err := hgy.Marshal()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(gypath, hgyData, 0644)
	if err != nil {
		return err
	}

	installer := repo.NewInstaller()
	installer.ResolveAllFiles = false
	installer.ResolveTest = true
	installer.Home = home

	// NOTE(russell_h): The pattern for error checking here seems to be to check
	// msg.HasErrored(), but there isn't currently anything we would do differently
	// if an error has occurred.
	action.Update(installer, false, true)

	return nil
}

func Update(c *cli.Context) error {
	gypath := c.GlobalString("yaml")
	glpath := c.GlobalString("lock")
	home := c.GlobalString("home")

	return doUpdate(gypath, glpath, home, func(lock *cfg.Lock) *cfg.Dependency {
		dep := cfg.DependencyFromLock(lock)
		dep.Reference = "master"
		return dep
	})
}

func NewDep(c *cli.Context) error {
	gypath := c.GlobalString("yaml")
	glpath := c.GlobalString("lock")
	home := c.GlobalString("home")

	return doUpdate(gypath, glpath, home, cfg.DependencyFromLock)
}

const usage = `A wrapper around glide to enable a workflow where:

     - every dependency is frequently updated
     - individual dependencies can be easily added`

func main() {
	app := cli.NewApp()
	app.Name = "hyperglide"
	app.Usage = usage
	app.Version = "master"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "yaml, y",
			Value: "glide.yaml",
			Usage: "Path to a glide.yaml configuration",
		},
		cli.StringFlag{
			Name:  "lock, l",
			Value: "glide.lock",
			Usage: "Path to the Glide lock file",
		},
		cli.StringFlag{
			Name:   "home",
			Value:  gpath.Home(),
			Usage:  "The location of Glide files",
			EnvVar: "GLIDE_HOME",
		},
	}

	app.Before = startup
	app.After = shutdown

	app.Commands = []cli.Command{
		{
			Name:      "update",
			ShortName: "up",
			Usage:     "Update everything",
			Action:    Update,
		},
		{
			Name:   "newdep",
			Usage:  "Vendor new dependencies, don't update the others",
			Action: NewDep,
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		if err != ErrAlreadyReportedError {
			fmt.Fprintf(os.Stderr, "error: %s", err.Error())
		}

		os.Exit(1)
	}
}

func startup(c *cli.Context) error {
	action.Init(c.GlobalString("yaml"), c.GlobalString("home"))
	action.EnsureGoVendor()
	return nil
}

func shutdown(c *cli.Context) error {
	cache.SystemUnlock()
	return nil
}