package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/karalabe/hid"
	"github.com/tez-capital/tezos-ledger-check/ledger"
	"github.com/urfave/cli/v2"
)

func main() {

	app := &cli.App{
		Name:    "ledger-check",
		Usage:   "Scan Ledger devices and display their app version and ledger id.",
		Version: VERSION,
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
				Name:    "bus",
				Usage:   "Filter by bus (first part of HID path)",
				Aliases: []string{"b"},
			},
			&cli.StringFlag{
				Name:    "address",
				Usage:   "Filter by address (second part of HID path)",
				Aliases: []string{"a"},
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "Set the log level (debug, info, warn, error)",
				Aliases: []string{"l"},
				Value:   "info",
			},
		},
		Action: func(c *cli.Context) error {
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

			runLedgerCheck(c.String("bus"), c.String("address"))
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		// Log the error and exit.
		log.Fatal(err)
	}

}

func runLedgerCheck(bus, address string) {
	hids := hid.Enumerate(11415, 0)

	for _, d := range hids {
		if d.VendorID != ledger.LEDGER_VENDOR_ID {
			continue
		}

		if d.Interface != 0 {
			slog.Debug("skipping non default interface", "interface", d.Interface)
			continue
		}

		parts := strings.Split(d.Path, ":")
		if len(parts) < 3 {
			slog.Debug("skipping invalid path", "path", d.Path)
			continue
		}

		if bus != "" {
			devBus, err := strconv.ParseUint(parts[0], 16, 64)
			if err != nil {
				slog.Error("failed to parse device bus", "bus", parts[0], "error", err.Error())
				os.Exit(1)
			}
			desiredBus, err := strconv.ParseUint(bus, 16, 64)
			if err != nil {
				slog.Error("failed to parse desired bus", "bus", bus, "error", err.Error())
				os.Exit(1)
			}

			if devBus != desiredBus {
				slog.Debug("skipping bus", "bus", parts[0])
				continue
			}
		}

		if address != "" && parts[1] != address {
			devAddress, err := strconv.ParseUint(parts[1], 16, 64)
			if err != nil {
				slog.Error("failed to parse device address", "address", parts[1], "error", err.Error())
				os.Exit(1)
			}
			desiredaddress, err := strconv.ParseUint(address, 16, 64)
			if err != nil {
				slog.Error("failed to parse desired address", "address", address, "error", err.Error())
				os.Exit(1)
			}

			if devAddress != desiredaddress {
				slog.Debug("skipping address", "address", parts[1])
				continue
			}
		}

		device, err := d.Open()
		if err != nil {
			panic(err)
		}
		func() {
			defer device.Close()

			ledgerId, appVersion, authorizedPath := "-", "-", "-"
			ledgerId, err = ledger.GetLedgerId(device)
			if err != nil {
				slog.Debug("failed to get ledger id", "error", err.Error())
			} else {
				appVersion, err = ledger.GetAppVersion(device)
				if err != nil {
					appVersion = err.Error()
				}
				authorizedPath, err = ledger.GetAuthorizedPath(device)
				if err != nil {
					authorizedPath = err.Error()
				}
			}

			fmt.Printf("%s,%s,%s,%s:%s\n", ledgerId, appVersion, authorizedPath, parts[0] /* bus */, parts[1] /* address */)
		}()
	}
}
