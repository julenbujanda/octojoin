// Copyright 2025 Matthew Gall <me@matthewgall.dev>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// envBool returns the boolean value of an environment variable, or def when it isn't set
func envBool(name string, def bool) bool {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Fatalf("Invalid value for %s: %q (use true or false)", name, v)
	}
	return b
}

// envInt returns the integer value of an environment variable, or def when it isn't set
func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("Invalid value for %s: %q (use a whole number)", name, v)
	}
	return i
}

func main() {
	var accountID, apiKey, configPath, ntfyURL string
	var daemon, webUI, debug, showVersion, noSmartIntervals, runTest, testNtfy bool
	var minPoints, webPort, happyHoursPerDay int
	var happyHourTimes, happyHourSlot string
	
	flag.StringVar(&configPath, "config", os.Getenv("OCTOJOIN_CONFIG"), "Path to configuration file")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.StringVar(&accountID, "account", os.Getenv("OCTOPUS_ACCOUNT_ID"), "Octopus Energy Account ID")
	flag.StringVar(&apiKey, "key", os.Getenv("OCTOPUS_API_KEY"), "Octopus Energy API Key")
	flag.BoolVar(&daemon, "daemon", envBool("OCTOJOIN_DAEMON", false), "Run in daemon mode (continuous monitoring)")
	flag.BoolVar(&webUI, "web", envBool("OCTOJOIN_WEB", false), "Enable web UI dashboard (daemon mode only)")
	flag.BoolVar(&debug, "debug", envBool("OCTOJOIN_DEBUG", false), "Enable debug logging")
	flag.IntVar(&minPoints, "min-points", envInt("OCTOJOIN_MIN_POINTS", 0), "Minimum points threshold to join a session (0 = join all sessions)")
	flag.IntVar(&webPort, "port", envInt("OCTOJOIN_PORT", 8080), "Web UI port (default: 8080)")
	flag.BoolVar(&noSmartIntervals, "no-smart-intervals", envBool("OCTOJOIN_NO_SMART_INTERVALS", false), "Disable smart interval adjustment (use fixed intervals)")
	flag.StringVar(&ntfyURL, "ntfy-url", os.Getenv("NTFY_URL"), "ntfy topic URL for push notifications")
	flag.IntVar(&happyHoursPerDay, "happy-hours-per-day", envInt("OCTOJOIN_HAPPY_HOURS_PER_DAY", 1), "Weekend Happy Hour slots to book per day, 0-2 (0 = notify only)")
	flag.StringVar(&happyHourTimes, "happy-hour-times", os.Getenv("OCTOJOIN_HAPPY_HOUR_TIMES"), "Comma-separated preferred Weekend Happy Hour start times, UK time (e.g. 11:00,12:00)")
	flag.StringVar(&happyHourSlot, "happy-hour-slot", os.Getenv("OCTOJOIN_HAPPY_HOUR_SLOT"), "Weekend Happy Hour slot to book when no preferred time has space: latest or earliest (default latest)")
	flag.BoolVar(&runTest, "test", false, "Run compatibility test to verify OctoJoin requirements and exit")
	flag.BoolVar(&testNtfy, "test-ntfy", false, "Send a test ntfy notification and exit")
	flag.Parse()

	// Handle version flag
	if showVersion {
		fmt.Printf("octojoin %s\n", GetVersion())
		fmt.Printf("User-Agent: %s\n", GetUserAgent())
		os.Exit(0)
	}

	// Load configuration file if provided
	config, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config file: %v", err)
	}
	config.ApplyDefaults()

	// Command line arguments and environment variables override config file
	if accountID == "" && config.AccountID != "" {
		accountID = config.AccountID
	}
	if apiKey == "" && config.APIKey != "" {
		apiKey = config.APIKey
	}
	if !daemon && config.Daemon {
		daemon = config.Daemon
	}
	if !webUI && config.WebUI {
		webUI = config.WebUI
	}
	if !debug && config.Debug {
		debug = config.Debug
	}
	if minPoints == 0 && config.MinPoints != 0 {
		minPoints = config.MinPoints
	}
	if webPort == 8080 && config.WebPort != 8080 && config.WebPort > 0 {
		webPort = config.WebPort
	}

	if ntfyURL == "" && config.NtfyURL != "" {
		ntfyURL = config.NtfyURL
	}
	if happyHoursPerDay == 1 && config.HappyHoursPerDay != 1 {
		happyHoursPerDay = config.HappyHoursPerDay
	}
	if happyHourTimes != "" {
		config.HappyHourTimes = nil
		for _, t := range strings.Split(happyHourTimes, ",") {
			if t = strings.TrimSpace(t); t != "" {
				config.HappyHourTimes = append(config.HappyHourTimes, t)
			}
		}
	}
	if happyHourSlot != "" {
		config.HappyHourSlot = happyHourSlot
	}
	ntfyToken := os.Getenv("NTFY_TOKEN")
	if ntfyToken == "" {
		ntfyToken = config.NtfyToken
	}

	// Update config with final values for validation
	config.AccountID = accountID
	config.APIKey = apiKey
	config.Daemon = daemon
	config.WebUI = webUI
	config.WebPort = webPort
	config.MinPoints = minPoints
	config.NtfyURL = ntfyURL
	config.HappyHoursPerDay = happyHoursPerDay

	// Validate configuration
	if err := config.Validate(); err != nil {
		log.Fatal(err)
	}

	if testNtfy {
		if ntfyURL == "" {
			fmt.Println("❌ ntfy is not configured: set NTFY_URL, -ntfy-url or ntfy_url")
			os.Exit(1)
		}
		if err := NewNtfyNotifier(ntfyURL, ntfyToken).SendTest(); err != nil {
			fmt.Printf("❌ Failed to send test notification: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ Test notification sent to %s\n", ntfyURL)
		return
	}

	if accountID == "" || apiKey == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -account=<account_id> -key=<api_key>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Or set environment variables: OCTOPUS_ACCOUNT_ID and OCTOPUS_API_KEY\n")
		fmt.Fprintf(os.Stderr, "Or use a configuration file with -config=<path>\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Initialize logger for main application
	logger := NewLogger(debug).WithComponent("main")

	logger.Info("Starting Octopus Energy Saving Session Monitor",
		"version", GetVersion(),
	)
	logger.Debug("Configuration",
		"account_id_prefix", accountID[:min(5, len(accountID))],
		"daemon_mode", daemon,
		"web_ui", webUI,
		"min_points", minPoints,
	)

	// Initialize API client
	client := NewOctopusClient(accountID, apiKey, debug)
	
	// Handle compatibility testing flag
	if runTest {
		fmt.Println("🔍 Running OctoJoin Compatibility Test...")
		fmt.Println("===========================================")
		
		// Initialize state for caching
		monitor := NewSavingSessionMonitor(client, accountID)
		testPassed := true
		
		// Test 1: Basic API connectivity and account info
		fmt.Println("\n1️⃣  Testing API connectivity and account access...")
		accountInfo, err := client.getAccountInfo()
		if err != nil {
			fmt.Printf("❌ Failed to access account information: %v", err)
			testPassed = false
		} else {
			fmt.Printf("✅ Account access successful")
			fmt.Printf("   Balance: £%.2f", accountInfo.Balance)
			fmt.Printf("   Account Type: %s", accountInfo.AccountType)
		}
		
		// Test 2: Saving Sessions API
		fmt.Println("\n2️⃣  Testing Saving Sessions API...")
		sessions, err := client.GetSavingSessions()
		if err != nil {
			fmt.Printf("❌ Failed to access Saving Sessions: %v", err)
			testPassed = false
		} else {
			fmt.Printf("✅ Saving Sessions API accessible")
			fmt.Printf("   Current OctoPoints: %d", sessions.Data.OctoPoints.Account.CurrentPointsInWallet)
			fmt.Printf("   Joined sessions: %d", len(sessions.Data.SavingSessions.Account.JoinedEvents))
			fmt.Printf("   Upcoming events: %d", len(sessions.Data.SavingSessions.Events))
			fmt.Printf("   Campaign enrolled: %t", sessions.Data.SavingSessions.Account.HasJoinedCampaign)
			fmt.Printf("   Weekend Happy Hours to book: %d (%d tokens)", sessions.HappyHoursAvailable(), sessions.Data.SavingSessions.Account.TokenBalance)
			
			if !sessions.Data.SavingSessions.Account.HasJoinedCampaign {
				fmt.Printf("⚠️  Warning: Not enrolled in Saving Sessions campaign")
			}
		}
		
		// Test 3: Campaign status
		fmt.Println("\n3️⃣  Testing campaign enrollment status...")
		campaigns, err := client.getCampaignStatus()
		if err != nil {
			fmt.Printf("❌ Failed to check campaign status: %v", err)
			testPassed = false
		} else {
			fmt.Printf("✅ Campaign status accessible")
			enrolledCount := 0
			for campaign, enrolled := range campaigns {
				status := "❌ Not enrolled"
				if enrolled {
					status = "✅ Enrolled"
					enrolledCount++
				}
				fmt.Printf("   %s: %s", campaign, status)
			}
			
			if enrolledCount == 0 {
				fmt.Printf("⚠️  Warning: Not enrolled in any campaigns")
			}
		}
		
		// Test 4: Free Electricity Sessions
		fmt.Println("\n4️⃣  Testing Free Electricity Sessions...")
		freeElectricity, err := client.GetFreeElectricitySessions()
		if err != nil {
			fmt.Printf("❌ Failed to access Free Electricity Sessions: %v", err)
			testPassed = false
		} else {
			fmt.Printf("✅ Free Electricity Sessions API accessible")
			fmt.Printf("   Available sessions: %d", len(freeElectricity.Data))
		}
		
		// Test 5: Smart meter device discovery
		fmt.Println("\n5️⃣  Testing smart meter device discovery...")
		devices, err := client.getSmartMeterDevicesWithCache(monitor.state)
		if err != nil {
			fmt.Printf("❌ Failed to discover smart meter devices: %v", err)
			testPassed = false
		} else {
			fmt.Printf("✅ Found %d ESME (smart meter) devices:", len(devices))
			for i, device := range devices {
				fmt.Printf("   %d. %s", i+1, device)
			}
			
			if len(devices) == 0 {
				fmt.Printf("⚠️  Warning: No smart meter devices found - usage graphs will not work")
			}
		}
		
		// Test 6: Usage measurements (if smart meter available)
		if len(devices) > 0 {
			fmt.Println("\n6️⃣  Testing smart meter data retrieval...")
			measurements, err := client.getUsageMeasurementsWithCache(monitor.state, 7)
			if err != nil {
				fmt.Printf("❌ Failed to retrieve usage measurements: %v", err)
				testPassed = false
			} else {
				fmt.Printf("✅ Retrieved %d usage measurements for last 7 days", len(measurements))
				if len(measurements) > 0 {
					fmt.Printf("   Sample measurements:")
					for i, m := range measurements[:min(3, len(measurements))] {
						fmt.Printf("     %d. %s: %.3f %s", 
							i+1, m.StartAt.Format("2006-01-02 15:04"), 
							m.GetValueAsFloat64(), m.Unit)
					}
				} else {
					fmt.Printf("⚠️  Warning: No usage data available - usage graphs will be empty")
				}
			}
		}
		
		// Test 7: Wheel of Fortune spins
		fmt.Println("\n7️⃣  Testing Wheel of Fortune...")
		spins, err := client.getWheelOfFortuneSpins()
		if err != nil {
			fmt.Printf("❌ Failed to check Wheel of Fortune spins: %v", err)
			testPassed = false
		} else {
			fmt.Printf("✅ Wheel of Fortune accessible")
			fmt.Printf("   Electricity spins: %d", spins.ElectricitySpins)
			fmt.Printf("   Gas spins: %d", spins.GasSpins)
		}
		
		// Test 8: ntfy notifications
		fmt.Println("\n8️⃣  Testing ntfy notifications...")
		if ntfyURL == "" {
			fmt.Println("⏭️  ntfy not configured, skipping")
		} else if err := NewNtfyNotifier(ntfyURL, ntfyToken).SendTest(); err != nil {
			fmt.Printf("❌ Failed to send test notification: %v\n", err)
			testPassed = false
		} else {
			fmt.Println("✅ Test notification sent - check your ntfy app")
		}

		// Final results
		fmt.Println("\n===========================================")
		if testPassed {
			fmt.Println("🎉 All tests passed! OctoJoin should work perfectly for your account.")
			fmt.Println("   You can now run OctoJoin in daemon mode with: octojoin -daemon")
		} else {
			fmt.Println("❌ Some tests failed. Please check your credentials and account setup.")
			fmt.Println("   Verify your account ID and API key are correct.")
		}
		fmt.Println("===========================================")
		
		return
	}
	
	// Check for updates in background (non-blocking)
	go PrintUpdateNotification()

	// Initialize monitor
	monitor := NewSavingSessionMonitor(client, accountID)
	monitor.SetMinPointsThreshold(minPoints)

	// Configure smart intervals (command line flag takes precedence over config)
	disableSmartIntervals := noSmartIntervals || config.NoSmartIntervals
	monitor.SetSmartIntervals(!disableSmartIntervals)
	
	// Set custom check interval if specified in config
	if config.CheckInterval > 0 && config.CheckInterval != 10 {
		monitor.SetCheckInterval(time.Duration(config.CheckInterval) * time.Minute)
		logger.Info("Using custom check interval", "interval_minutes", config.CheckInterval)
	}

	// Enable web UI if requested and in daemon mode
	if webUI && daemon {
		monitor.SetDaemonMode(true) // Use structured logging for daemon mode
		monitor.EnableWebUI(webPort)
		logger.Info("Web UI enabled", "url", fmt.Sprintf("http://localhost:%d", webPort))
	} else if webUI && !daemon {
		logger.Warn("Web UI can only be enabled in daemon mode")
	}

	monitor.SetHappyHourBooking(happyHoursPerDay, config.HappyHourTimes, config.HappyHourSlot)
	if happyHoursPerDay > 0 {
		logger.Info("Weekend Happy Hour booking enabled", "per_day", happyHoursPerDay, "preferred_times", strings.Join(config.HappyHourTimes, ","), "slot", config.HappyHourSlot)
	}

	if ntfyURL != "" {
		monitor.SetNotifier(NewNtfyNotifier(ntfyURL, ntfyToken))
		logger.Info("ntfy notifications enabled")
	}

	if minPoints > 0 {
		logger.Info("Minimum points threshold set", "min_points", minPoints)
	} else {
		logger.Info("No minimum points threshold - will join all sessions")
	}
	
	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT (Ctrl+C) and SIGTERM (systemd stop)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	if daemon {
		logger.Info("Running in daemon mode - continuous monitoring")

		// Start monitor in goroutine
		go func() {
			if err := monitor.StartWithContext(ctx); err != nil && err != context.Canceled {
				logger.Error("Monitor error", "error", err.Error())
			}
		}()

		// Wait for shutdown signal
		sig := <-sigCh
		logger.Info("Received signal, initiating graceful shutdown", "signal", sig.String())

		// Cancel context to stop monitor
		cancel()

		// Give monitor time to finish current operations
		time.Sleep(2 * time.Second)
		logger.Info("Shutdown complete")
	} else {
		logger.Info("Running in one-shot mode")
		monitor.CheckOnce()
	}
}