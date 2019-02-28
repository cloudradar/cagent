package cagent

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/troian/toml"
)

var DefaultCfgPath string
var defaultLogPath string
var rootCertsPath string

var configAutogeneratedHeadline = []byte(
	`# This is an auto-generated config to connect with the cloudradar service
# To see all options of cagent run cagent -p

`)

type MinValuableConfig struct {
	LogLevel LogLevel `toml:"log_level" comment:"\"debug\", \"info\", \"error\" verbose level; can be overridden with -v flag"`

	HubURL      string `toml:"hub_url" commented:"true"`
	HubUser     string `toml:"hub_user" commented:"true"`
	HubPassword string `toml:"hub_password" commented:"true"`
}

type Config struct {
	Interval float64 `toml:"interval" comment:"interval to push metrics to the HUB"`

	PidFile string `toml:"pid" comment:"pid file location"`
	LogFile string `toml:"log,omitempty" required:"false" comment:"log file location"`

	MinValuableConfig

	HubGzip          bool   `toml:"hub_gzip" comment:"enable gzip when sending results to the HUB"`
	HubProxy         string `toml:"hub_proxy" commented:"true"`
	HubProxyUser     string `toml:"hub_proxy_user" commented:"true"`
	HubProxyPassword string `toml:"hub_proxy_password" commented:"true"`

	CPULoadDataGather []string `toml:"cpu_load_data_gathering_mode" comment:"default ['avg1']"`
	CPUUtilDataGather []string `toml:"cpu_utilisation_gathering_mode" comment:"default ['avg1']"`
	CPUUtilTypes      []string `toml:"cpu_utilisation_types" comment:"default ['user','system','idle','iowait']"`

	FSTypeInclude []string `toml:"fs_type_include" comment:"default ['ext3','ext4','xfs','jfs','ntfs','btrfs','hfs','apfs','fat32']"`
	FSPathExclude []string `toml:"fs_path_exclude" comment:"default []"`
	FSMetrics     []string `toml:"fs_metrics" comment:"default ['free_B','free_percent','total_B']"`

	NetInterfaceExclude             []string `toml:"net_interface_exclude" commented:"true"`
	NetInterfaceExcludeRegex        []string `toml:"net_interface_exclude_regex" comment:"default [], default on windows: [\"Pseudo-Interface\"]"`
	NetInterfaceExcludeDisconnected bool     `toml:"net_interface_exclude_disconnected" comment:"default true"`
	NetInterfaceExcludeLoopback     bool     `toml:"net_interface_exclude_loopback" comment:"default true"`

	NetMetrics []string `toml:"net_metrics" comment:"default['in_B_per_s', 'out_B_per_s']"`

	SystemFields []string `toml:"system_fields" comment:"default ['uname','os_kernel','os_family','os_arch','cpu_model','fqdn','memory_total_B']"`

	WindowsUpdatesWatcherInterval int `toml:"windows_updates_watcher_interval" comment:"default 3600"`

	VirtualMachinesStat []string `toml:"virtual_machines_stat" comment:"default ['hyper-v'], available options 'hyper-v'"`

	HardwareInventory bool `toml:"hardware_inventory" comment:"default true"`

	DiscoverAutostartingServicesOnly bool `toml:"discover_autostarting_services_only" comment:"default true"`

	CPUUtilisationAnalysis CPUUtilisationAnalysis `toml:"cpu_utilisation_analysis"`
}

type CPUUtilisationAnalysis struct {
	Threshold                      float64 `toml:"threshold" comment:"target value to start the analysis" json:"threshold"`
	Function                       string  `toml:"function" comment:"threshold compare function, possible values: 'lt', 'lte', 'gt', 'gte'" json:"function"`
	Metric                         string  `toml:"metric" commend:"possible values: 'user','system','idle','iowait'" json:"metric"`
	GatheringMode                  string  `toml:"gathering_mode" comment:"should be one of values of cpu_utilisation_gathering_mode" json:"gathering_mode"`
	ReportProcesses                int     `toml:"report_processes" comment:"number of processes to return" json:"report_processes"`
	TrailingProcessAnalysisMinutes int     `toml:"trailing_process_analysis_minutes" comment:"how much time analysis will continue to perform after the CPU utilisation returns to the normal value" json:"trailing_process_analysis_minutes"`
}

func init() {
	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)

	switch runtime.GOOS {
	case "windows":
		DefaultCfgPath = filepath.Join(exPath, "./cagent.conf")
		defaultLogPath = filepath.Join(exPath, "./cagent.log")
	case "darwin":
		DefaultCfgPath = os.Getenv("HOME") + "/.cagent/cagent.conf"
		defaultLogPath = os.Getenv("HOME") + "/.cagent/cagent.log"
	default:
		rootCertsPath = "/etc/cagent/cacert.pem"
		DefaultCfgPath = "/etc/cagent/cagent.conf"
		defaultLogPath = "/var/log/cagent/cagent.log"
	}
}

func NewConfig() *Config {
	cfg := &Config{
		LogFile:                          defaultLogPath,
		Interval:                         90,
		CPULoadDataGather:                []string{"avg1"},
		CPUUtilTypes:                     []string{"user", "system", "idle", "iowait"},
		CPUUtilDataGather:                []string{"avg1"},
		FSTypeInclude:                    []string{"ext3", "ext4", "xfs", "jfs", "ntfs", "btrfs", "hfs", "apfs", "fat32"},
		FSMetrics:                        []string{"free_B", "free_percent", "total_B"},
		NetMetrics:                       []string{"in_B_per_s", "out_B_per_s"},
		NetInterfaceExcludeDisconnected:  true,
		NetInterfaceExclude:              []string{},
		NetInterfaceExcludeRegex:         []string{},
		NetInterfaceExcludeLoopback:      true,
		SystemFields:                     []string{"uname", "os_kernel", "os_family", "os_arch", "cpu_model", "fqdn", "memory_total_B"},
		HardwareInventory:                true,
		DiscoverAutostartingServicesOnly: true,
		CPUUtilisationAnalysis: CPUUtilisationAnalysis{
			Threshold:                      10,
			Function:                       "lt",
			Metric:                         "idle",
			GatheringMode:                  "avg1",
			ReportProcesses:                5,
			TrailingProcessAnalysisMinutes: 5,
		},
	}

	if runtime.GOOS == "windows" {
		cfg.WindowsUpdatesWatcherInterval = 3600
		cfg.NetInterfaceExcludeRegex = []string{"Pseudo-Interface"}
		cfg.CPULoadDataGather = []string{}
		cfg.CPUUtilTypes = []string{"user", "system", "idle"}
		cfg.VirtualMachinesStat = []string{"hyper-v"}
	}

	return cfg
}

func NewMinimumConfig() *MinValuableConfig {
	cfg := &MinValuableConfig{
		LogLevel: "error",
	}

	cfg.applyEnv(false)

	return cfg
}

func secToDuration(secs float64) time.Duration {
	return time.Duration(int64(float64(time.Second) * secs))
}

func (mvc *MinValuableConfig) applyEnv(force bool) {
	if val, ok := os.LookupEnv("CAGENT_HUB_URL"); ok && ((mvc.HubURL == "") || force) {
		mvc.HubURL = val
	}

	if val, ok := os.LookupEnv("CAGENT_HUB_USER"); ok && ((mvc.HubUser == "") || force) {
		mvc.HubUser = val
	}

	if val, ok := os.LookupEnv("CAGENT_HUB_PASSWORD"); ok && ((mvc.HubPassword == "") || force) {
		mvc.HubPassword = val
	}
}

func (cfg *Config) DumpToml() string {
	buff := &bytes.Buffer{}

	err := toml.NewEncoder(buff).Encode(cfg)
	if err != nil {
		log.Errorf("DumpToml error: %s", err.Error())
		return ""
	}

	return buff.String()
}

// TryUpdateConfigFromFile applies values from file in configFilePath to cfg if given file exists.
// it rewrites all cfg keys that present in the file
func TryUpdateConfigFromFile(cfg *Config, configFilePath string) error {
	_, err := os.Stat(configFilePath)
	if err != nil {
		return err
	}

	_, err = toml.DecodeFile(configFilePath, cfg)
	if err != nil {
		return err
	}

	// log.Printf("WARP: %+v", cfg)

	return nil
}

func SaveConfigFile(cfg interface{}, configFilePath string) error {
	var f *os.File
	var err error
	if f, err = os.OpenFile(configFilePath, os.O_WRONLY|os.O_CREATE, 0666); err != nil {
		return fmt.Errorf("failed to open the config file: '%s'", configFilePath)
	}

	defer func() {
		if err = f.Close(); err != nil {
			log.WithError(err).Errorf("failed to close config file: %s", configFilePath)
		}
	}()

	if _, err = f.Write(configAutogeneratedHeadline); err != nil {
		return fmt.Errorf("failed to write headline to config file")
	}

	err = toml.NewEncoder(f).Encode(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode config to file")
	}

	return nil
}

func GenerateDefaultConfigFile(mvc *MinValuableConfig, configFilePath string) error {
	var err error

	if _, err = os.Stat(configFilePath); os.IsExist(err) {
		return fmt.Errorf("сonfig file already exists at path: %s", configFilePath)
	}

	configPathDir := filepath.Dir(configFilePath)
	if _, err := os.Stat(configPathDir); os.IsNotExist(err) {
		err := os.MkdirAll(configPathDir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed to auto-create the default сonfig file directory '%s': %s", configPathDir, err.Error())
		}
	}

	var f *os.File
	if f, err = os.OpenFile(configFilePath, os.O_WRONLY|os.O_CREATE, 0666); err != nil {
		return fmt.Errorf("failed to create the default сonfig file at '%s': %s", configFilePath, err.Error())
	}

	defer func() {
		if err = f.Close(); err != nil {
			log.WithError(err).Errorf("failed to close сonfig file: %s", configFilePath)
		}
	}()

	if _, err = f.Write(configAutogeneratedHeadline); err != nil {
		return fmt.Errorf("failed to write headline to сonfig file")
	}

	err = toml.NewEncoder(f).Encode(mvc)
	if err != nil {
		return fmt.Errorf("failed to encode сonfig to file")
	}

	return err
}

func (cfg *Config) validate() error {
	if cfg.HubProxy != "" {
		if !strings.HasPrefix(cfg.HubProxy, "http") {
			cfg.HubProxy = "http://" + cfg.HubProxy
		}

		if _, err := url.Parse(cfg.HubProxy); err != nil {
			return fmt.Errorf("failed to parse 'hub_proxy' URL")
		}
	}

	return nil
}

// HandleAllConfigSetup prepares Config for Cagent with parameters specified in file
// if Config file does not exist default one is created in form of MinValuableConfig
func HandleAllConfigSetup(configFilePath string) (*Config, error) {
	cfg := NewConfig()

	err := TryUpdateConfigFromFile(cfg, configFilePath)
	// If the Config file does not exist create a default Config at configFilePath
	if os.IsNotExist(err) {
		mvc := NewMinimumConfig()
		if err = GenerateDefaultConfigFile(mvc, configFilePath); err != nil {
			return nil, err
		}

		cfg.MinValuableConfig = *mvc
	} else if err != nil {
		if strings.Contains(err.Error(), "cannot load TOML value of type int64 into a Go float") {
			return nil, fmt.Errorf("Config load error: please use numbers with a decimal point for numerical values")
		}
		return nil, fmt.Errorf("Config load error: %s", err.Error())
	}

	if err = cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
