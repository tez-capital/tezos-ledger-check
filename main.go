package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/karalabe/hid"
	"github.com/tez-capital/tezos-ledger-check/ledger"
	"github.com/urfave/cli/v2"
)

func main() {

	app := &cli.App{
		Name:        "ledger-check",
		Usage:       "Scan Ledger devices and display their app version and ledger id.",
		HideVersion: true,
		Commands: []*cli.Command{
			{
				Name:    "version",
				Aliases: []string{"v"},
				Usage:   "Print the CLI version",
				Action: func(c *cli.Context) error {
					fmt.Println(VERSION)
					return nil
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "path",
				Usage:   "Filter by HID path",
				Aliases: []string{"b"},
			},
			&cli.StringFlag{
				Name:  "ledger-id",
				Usage: "Filter by ledger id",
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "Set the log level (debug, info, warn, error)",
				Aliases: []string{"l"},
				Value:   "info",
			},
			&cli.BoolFlag{
				Name:  "version",
				Usage: "Prints the version",
			},
		},
		Action: func(c *cli.Context) error {
			if c.Bool("version") {
				fmt.Println(VERSION)
				return nil
			}

			logLevel := c.String("log-level")
			switch logLevel {
			case "debug":
				slog.SetLogLoggerLevel(slog.LevelDebug)
			case "info":
				slog.SetLogLoggerLevel(slog.LevelInfo)
			case "warn":
				slog.SetLogLoggerLevel(slog.LevelWarn)
			case "error":
				slog.SetLogLoggerLevel(slog.LevelError)
			default:
				log.Fatalf("Invalid log level: %s", logLevel)
			}

			runLedgerCheck(c.String("path"), c.String("ledger-id"))
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		// Log the error and exit.
		log.Fatal(err)
	}

}

func runLedgerCheck(pathString, desiredLedgerId string) {
	if !hid.Supported() {
		slog.Error("HID not supported")
		os.Exit(1)
	}

	desiredPaths := []string{}
	if pathString != "" {
		pathStrings := strings.Split(pathString, ",")
		desiredPaths = append(desiredPaths, pathStrings...)
	}

	desiredLedgerIds := []string{}
	if desiredLedgerId != "" {
		desiredLedgerIds = strings.Split(desiredLedgerId, ",")
	}

	hids, err := hid.Enumerate(0, 0)
	if err != nil {
		slog.Error("failed to enumerate HID devices", "error", err.Error())
		os.Exit(1)
	}

	for _, d := range hids {
		slog.Debug("found device", "interface", d.Interface, "vendor_id", d.VendorID, "product_id", d.ProductID, "path", d.Path)
		if !ledger.IsLedger(d.VendorID) {
			slog.Debug("skipping non-Ledger device", "vendor_id", d.VendorID, "product_id", d.ProductID, "path", d.Path)
			continue
		}

		if len(desiredPaths) > 0 {
			if !slices.Contains(desiredPaths, d.Path) {
				slog.Debug("skipping path", "path", d.Path)
				continue
			}
		}

		func() {
			device, err := d.Open()
			if err != nil {
				slog.Debug("failed to open device", "error", err.Error())
				return
			}
			defer device.Close()

			ledgerId, appVersion, authorizedPath := "-", "-", "-"
			ledgerId, err = ledger.GetLedgerId(device)
			if err != nil {
				slog.Debug("failed to get ledger id", "error", err.Error())
				return
			} else {
				if len(desiredLedgerIds) > 0 && !slices.Contains(desiredLedgerIds, ledgerId) {
					return
				}
				appVersion, err = ledger.GetAppVersion(device)
				if err != nil {
					appVersion = fmt.Sprintf("-,%s", err.Error())
				}
				authorizedPath, err = ledger.GetAuthorizedPath(device)
				if err != nil {
					authorizedPath = fmt.Sprintf("-,%s", err.Error())
				}
			}

			fmt.Printf("%s;%s;%s;%s\n", ledgerId, appVersion, authorizedPath, d.Path)
		}()
	}
}
