/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/NVIDIA/gpu-operator/internal/licenseinfo"
)

const (
	licenseQueryCommand = "nvidia-smi"
	licenseQueryArgs    = "vgpu -q"
	licenseSource       = "nvidia-smi vgpu -q"
)

var (
	gpuHeaderRegexp  = regexp.MustCompile(`^GPU\s+([0-9A-Fa-fx:.]+)`)
	keyValueRegexp   = regexp.MustCompile(`^([^:]+):\s*(.*)$`)
	expiryMarker     = "Expiry:"
	licenseTimeParse = []string{
		"2006-1-2 15:04:05 MST",
		"2006-01-02 15:04:05 MST",
		"2006-1-2 15:04:05",
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
	}
)

// collectLicenseSnapshot runs nvidia-smi and parses vGPU license information.
// Errors are propagated and also captured in the returned snapshot so that callers
// can still surface diagnostic data to the cluster.
func collectLicenseSnapshot(ctx context.Context, now time.Time) (licenseinfo.Snapshot, error) {
	snapshot := licenseinfo.NewSnapshot(nil, licenseSource, now)

	output, err := runLicenseQuery(ctx)
	if err != nil {
		snapshot.Error = err.Error()
		return snapshot, err
	}

	devices, parseErr := parseVGPULicenseOutput(output)
	snapshot.Devices = devices
	if parseErr != nil {
		snapshot.Error = parseErr.Error()
		return snapshot, parseErr
	}
	if len(devices) == 0 {
		err := fmt.Errorf("no vGPU license information found in nvidia-smi output")
		snapshot.Error = err.Error()
		return snapshot, err
	}

	return snapshot, nil
}

func runLicenseQuery(ctx context.Context) (string, error) {
	args := strings.Split(licenseQueryArgs, " ")
	cmd := exec.CommandContext(ctx, licenseQueryCommand, args...)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", licenseSource, err, strings.TrimSpace(combined.String()))
	}
	return combined.String(), nil
}

func parseVGPULicenseOutput(output string) ([]licenseinfo.DeviceStatus, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var (
		devices []licenseinfo.DeviceStatus
		current *licenseinfo.DeviceStatus
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if matches := gpuHeaderRegexp.FindStringSubmatch(line); len(matches) == 2 {
			if current != nil {
				devices = append(devices, *current)
			}
			current = &licenseinfo.DeviceStatus{ID: matches[1]}
			continue
		}

		if current == nil {
			continue
		}

		key, value := parseKeyValue(line)
		switch key {
		case "Product Name":
			current.Product = value
		case "License Status":
			status, expiryCandidate := extractStatusAndExpiry(value)
			current.Status = status
			current.Licensed = isStatusLicensed(status)
			if expiryCandidate != "" && current.Expiry == nil {
				if ts, err := parseLicenseTimestamp(expiryCandidate); err == nil {
					current.Expiry = &ts
				} else if current.Message == "" {
					current.Message = fmt.Sprintf("license expiry: %s", expiryCandidate)
				}
			}
		case "License Expiry":
			if ts, err := parseLicenseTimestamp(value); err == nil {
				current.Expiry = &ts
			} else if current.Message == "" {
				current.Message = fmt.Sprintf("license expiry: %s", value)
			}
		case "vGPU Software Licensed":
			if !isAffirmative(value) {
				current.Licensed = false
			}
		}
	}

	if current != nil {
		devices = append(devices, *current)
	}

	if err := scanner.Err(); err != nil {
		return devices, fmt.Errorf("failed to parse license output: %w", err)
	}
	return devices, nil
}

func parseKeyValue(line string) (string, string) {
	matches := keyValueRegexp.FindStringSubmatch(line)
	if len(matches) != 3 {
		return line, ""
	}
	return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2])
}

func extractStatusAndExpiry(value string) (string, string) {
	idx := strings.Index(value, expiryMarker)
	if idx == -1 {
		return value, ""
	}
	status := strings.TrimSpace(strings.Trim(value[:idx], "()"))
	expiry := strings.TrimSpace(strings.Trim(value[idx+len(expiryMarker):], "()"))
	return status, expiry
}

func isStatusLicensed(status string) bool {
	if status == "" {
		return false
	}
	lower := strings.ToLower(status)
	if strings.Contains(lower, "unlicensed") || strings.Contains(lower, "not licensed") || strings.Contains(lower, "expired") {
		return false
	}
	return strings.Contains(lower, "licensed")
}

func isAffirmative(value string) bool {
	lower := strings.ToLower(value)
	return lower == "yes" || lower == "true"
}

func parseLicenseTimestamp(value string) (time.Time, error) {
	val := strings.TrimSpace(value)
	if val == "" || strings.EqualFold(val, "n/a") {
		return time.Time{}, fmt.Errorf("empty expiry")
	}
	for _, layout := range licenseTimeParse {
		if ts, err := time.Parse(layout, val); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported expiry format: %s", value)
}
