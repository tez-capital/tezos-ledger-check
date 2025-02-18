package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"slices"
	"strconv"
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

			runLedgerCheck(c.String("bus"), c.String("address"), c.String("ledger-id"))
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		// Log the error and exit.
		log.Fatal(err)
	}

}

func runLedgerCheck(busString, addressString, desiredLedgerId string) {
	if !hid.Supported() {
		slog.Error("HID not supported")
		os.Exit(1)
	}

	desiredBuses := []uint64{}
	if busString != "" {
		busStrings := strings.Split(busString, ",")
		for _, b := range busStrings {
			bus, err := strconv.ParseUint(b, 16, 64)
			if err != nil {
				slog.Error("failed to parse bus", "bus", b, "error", err.Error())
				os.Exit(1)
			}
			desiredBuses = append(desiredBuses, bus)
		}
	}

	desiredAddresses := []uint64{}
	if addressString != "" {
		addressStrings := strings.Split(addressString, ",")
		for _, a := range addressStrings {
			address, err := strconv.ParseUint(a, 16, 64)
			if err != nil {
				slog.Error("failed to parse address", "address", a, "error", err.Error())
				os.Exit(1)
			}
			desiredAddresses = append(desiredAddresses, address)
		}
	}

	desiredLedgerIds := []string{}
	if desiredLedgerId != "" {
		desiredLedgerIds = strings.Split(desiredLedgerId, ",")
	}

	hids := hid.Enumerate(0, 0)

	for _, d := range hids {
		if !ledger.IsLedger(d.VendorID) {
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

		if len(desiredBuses) > 0 {
			devBus, err := strconv.ParseUint(parts[0], 16, 64)
			if err != nil {
				slog.Error("failed to parse device bus", "bus", parts[0], "error", err.Error())
				os.Exit(1)
			}

			if !slices.Contains(desiredBuses, devBus) {
				slog.Debug("skipping bus", "bus", parts[0])
				continue
			}
		}

		if len(desiredAddresses) > 0 {
			devAddress, err := strconv.ParseUint(parts[1], 16, 64)
			if err != nil {
				slog.Error("failed to parse device address", "address", parts[1], "error", err.Error())
				os.Exit(1)
			}

			if !slices.Contains(desiredAddresses, devAddress) {
				slog.Debug("skipping address", "address", parts[1])
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
				ledgerId = fmt.Sprintf("-,%s", err.Error())
				if len(desiredLedgerIds) > 0 {
					return
				}
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

			fmt.Printf("%s;%s;%s;%s:%s\n", ledgerId, appVersion, authorizedPath, parts[0] /* bus */, parts[1] /* address */)
		}()
	}
}
