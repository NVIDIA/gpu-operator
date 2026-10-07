package certification

import (
	"os"
	"strings"
	"time"
)

const DefaultTimeout = 15 * time.Minute

type Config struct {
	Namespace              string
	Timeout                string
	TestSets               []string
	TestWorkloadImage      string
	TargetDriverRepository string
	TargetDriverImage      string
	TargetDriverVersion    string
	RDMABaseImage          string
}

// LoadConfigFromEnv builds a Config from environment variables.
func LoadConfigFromEnv() Config {
	var testSets []string
	if raw := os.Getenv("TEST_SETS"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(s); t != "" {
				testSets = append(testSets, t)
			}
		}
	}

	return Config{
		Namespace:              os.Getenv("NAMESPACE"),
		Timeout:                os.Getenv("TEST_TIMEOUT"),
		TestSets:               testSets,
		TestWorkloadImage:      os.Getenv("TEST_WORKLOAD_IMAGE"),
		TargetDriverRepository: os.Getenv("TARGET_DRIVER_REPOSITORY"),
		TargetDriverImage:      os.Getenv("TARGET_DRIVER_IMAGE"),
		TargetDriverVersion:    os.Getenv("TARGET_DRIVER_VERSION"),
		RDMABaseImage:          os.Getenv("RDMA_BASE_IMAGE"),
	}
}

func (c Config) ParseTimeout() time.Duration {
	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return DefaultTimeout
	}
	return timeout
}
