package cagent

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/troian/toml"

	"github.com/cloudradar-monitoring/cagent/pkg/common"
	"github.com/cloudradar-monitoring/cagent/pkg/jobmon"
	"github.com/cloudradar-monitoring/cagent/pkg/monitoring/mysql"
	"github.com/cloudradar-monitoring/cagent/pkg/monitoring/processes"
)

const (
	IOModeFile = "file"
	IOModeHTTP = "http"

	OperationModeFull      = "full"
	OperationModeMinimal   = "minimal"
	OperationModeHeartbeat = "heartbeat"

	minIntervalValue          = 30.0
	minHeartbeatIntervalValue = 5.0

	minHubRequestTimeout = 1
	maxHubRequestTimeout = 600

	minSystemUpdatesCheckInterval = 300
	minSelfUpdatesCheckInterval   = 600
)

var operationModes = []string{OperationModeFull, OperationModeMinimal, OperationModeHeartbeat}

var DefaultCfgPath string
var defaultLogPath string

var configAutogeneratedHeadline = []byte(
	`# This is an auto-generated config to connect with the cloudradar service
# To see all options of cagent run cagent -p

`)

type MinValuableConfig struct {
	LogLevel    LogLevel `toml:"log_level" comment:"\"debug\", \"info\", \"error\" verbose level; can be overridden with -v flag"`
	IOMode      string   `toml:"io_mode" commented:"true"`
	OutFile     string   `toml:"out_file,omitempty" comment:"output file path in io_mode=\"file\"\ncan be overridden with -o flag\non windows slash must be escaped\nfor example out_file = \"C:\\\\cagent.data.txt\""`
	HubURL      string   `toml:"hub_url" commented:"true"`
	HubUser     string   `toml:"hub_user" commented:"true"`
	HubPassword string   `toml:"hub_password" commented:"true"`
}

type LogsFilesConfig struct {
	HubFile string `toml:"hub_file,omitempty" comment:"log hub objects send to the hub"`
}

type Config struct {
	OperationMode     string  `toml:"operation_mode" comment:"operation_mode, possible values:\n\"full\": perform all checks unless disabled individually through other config option. Default.\n\"minimal\": perform just the checks for CPU utilization, CPU Load, Memory Usage, and Disk fill levels.\n\"heartbeat\": Just send the heartbeat according to the heartbeat interval.\nApplies only to io_mode = http, ignored on the command line."`
	Interval          float64 `toml:"interval" comment:"interval to push metrics to the HUB"`
	HeartbeatInterval float64 `toml:"heartbeat" comment:"send a heartbeat without metrics to the HUB every X seconds"`

	PidFile   string `toml:"pid" comment:"pid file location"`
	LogFile   string `toml:"log,omitempty" required:"false" comment:"log file location"`
	LogSyslog string `toml:"log_syslog" comment:"\"local\" for local unix socket or URL e.g. \"udp://localhost:514\" for remote syslog server"`

	MinValuableConfig

	HubGzip           bool   `toml:"hub_gzip" comment:"enable gzip when sending results to the HUB"`
	HubRequestTimeout int    `toml:"hub_request_timeout" comment:"time limit in seconds for requests made to Hub.\nThe timeout includes connection time, any redirects, and reading the response body.\nMin: 1, Max: 600. default: 30"`
	HubProxy          string `toml:"hub_proxy" commented:"true"`
	HubProxyUser      string `toml:"hub_proxy_user" commented:"true"`
	HubProxyPassword  string `toml:"hub_proxy_password" commented:"true"`

	CPULoadDataGather []string `toml:"cpu_load_data_gathering_mode" comment:"default ['avg1']"`
	CPUUtilDataGather []string `toml:"cpu_utilisation_gathering_mode" comment:"default ['avg1']"`
	CPUUtilTypes      []string `toml:"cpu_utilisation_types" comment:"default ['user','system','idle','iowait']"`

	FSTypeInclude                 []string `toml:"fs_type_include" comment:"default ['ext3','ext4','xfs','jfs','ntfs','btrfs','hfs','apfs','fat32','smbfs','nfs']"`
	FSPathExclude                 []string `toml:"fs_path_exclude" comment:"Exclude file systems by name, disabled by default"`
	FSPathExcludeRecurse          bool     `toml:"fs_path_exclude_recurse" comment:"Having fs_path_exclude_recurse = false the specified path must match a mountpoint or it will be ignored\nHaving fs_path_exclude_recurse = true the specified path can be any folder and all mountpoints underneath will be excluded"`
	FSMetrics                     []string `toml:"fs_metrics" comment:"default ['free_B', 'free_percent', 'total_B', 'read_B_per_s', 'write_B_per_s', 'read_ops_per_s', 'write_ops_per_s', 'inodes_used_percent']"`
	FSIdentifyMountpointsByDevice bool     `toml:"fs_identify_mountpoints_by_device" comment:"To avoid monitoring of so-called mount binds mount points are identified by the path and device name.\nMountpoints pointing to the same device are ignored. What appears first in /proc/self/mountinfo is considered as the original.\nApplies only to Linux"`

	NetInterfaceExclude             []string `toml:"net_interface_exclude" commented:"true"`
	NetInterfaceExcludeRegex        []string `toml:"net_interface_exclude_regex" comment:"default [\"^vnet(.*)$\", \"^virbr(.*)$\", \"^vmnet(.*)$\", \"^vEthernet(.*)$\"]. On Windows, also \"Pseudo-Interface\" is added to list"`
	NetInterfaceExcludeDisconnected bool     `toml:"net_interface_exclude_disconnected" comment:"default true"`
	NetInterfaceExcludeLoopback     bool     `toml:"net_interface_exclude_loopback" comment:"default true"`

	NetMetrics           []string `toml:"net_metrics" comment:"default ['in_B_per_s','out_B_per_s','total_out_B_per_s','total_in_B_per_s']"`
	NetInterfaceMaxSpeed string   `toml:"net_interface_max_speed" comment:"If the value is not specified, cagent will try to query the maximum speed of the network cards to calculate the bandwidth usage (default)\nDepending on the network card type this is not always reliable.\nSome virtual network cards, for example, report a maximum speed lower than the real speed.\nYou can set a fixed value by using <number of Bytes per second> + <K, M or G as a quantifier>.\nExamples: \"125M\" (equals 1 GigaBit), \"12.5M\" (equals 100 MegaBits), \"12.5G\" (equals 100 GigaBit)"`

	SystemFields []string `toml:"system_fields" comment:"default ['uname','os_kernel','os_family','os_arch','cpu_model','fqdn','memory_total_B']"`

	VirtualMachinesStat []string `toml:"virtual_machines_stat" comment:"default ['hyper-v'], available options 'hyper-v'"`

	HardwareInventory bool `toml:"hardware_inventory" comment:"default true"`

	DiscoverAutostartingServicesOnly bool `toml:"discover_autostarting_services_only" comment:"default true"`

	CPUUtilisationAnalysis CPUUtilisationAnalysisConfig `toml:"cpu_utilisation_analysis"`

	TemperatureMonitoring bool `toml:"temperature_monitoring" comment:"default true"`

	SoftwareRAIDMonitoring bool `toml:"software_raid_monitoring" comment:"Software raid monitoring\nAuto-detect software raids by reading /proc/mdstat and monitor them\ndefault true"`

	SMARTMonitoring bool            `toml:"smart_monitoring" comment:"Enable S.M.A.R.T monitoring of hard disks\ndefault false"`
	SMARTCtl        string          `toml:"smartctl" comment:"Path to a smartctl binary (smartctl.exe on windows, path must be escaped) version >= 7\nSee https://docs.cloudradar.io/configuring-hosts/installing-agents/troubleshoot-s.m.a.r.t-monitoring\nsmartctl = \"C:\\\\Program Files\\\\smartmontools\\\\bin\\\\smartctl.exe\"\nsmartctl = \"/usr/local/bin/smartctl\""`
	Logs            LogsFilesConfig `toml:"logs,omitempty"`

	StorCLI StorCLIConfig `toml:"storcli,omitempty" comment:"Enable monitoring of hardware health for MegaRaids\nreported by the storcli command-line tool\nRefer to https://docs.cloudradar.io/cagent/modules#storcli\nOn Linux make sure a sudo rule exists. The storcli command is always executed via sudo. Example:\ncagent ALL= NOPASSWD: /opt/MegaRAID/storcli/storcli64 /call show all J"`

	JobMonitoring JobMonitoringConfig `toml:"jobmon,omitempty" comment:"Settings for the jobmon wrapper for the job monitoring"`

	SystemUpdatesChecks UpdatesMonitoringConfig `toml:"system_updates_checks" comment:"Monitor the available updates using the operating system updates service\nUses apt-get, apt-check or yum, Requires sudo rules. DEB and RPM packages install them automatically.\nOn Windows, it requires windows updates to be switched on, ignored if windows updates are switched off"`

	MysqlMonitoring mysql.Config `toml:"mysql_monitoring" comment:"Monitor the basic performance metrics of a MySQL or MariaDB database\n** EXPERIMENTAL                          **\n** Do not use in production environments **"`

	ProcessMonitoring processes.Config `toml:"process_monitoring" comment:"Cagent monitors all running processes and reports them for further processing to the Hub.\nOn heavy loaded systems or if you don't need process monitoring at all,\nyou can change the following settings."`

	Updates UpdatesConfig `toml:"self_update" comment:"Control how cagent installs self-updates. Windows-only"`

	DockerMonitoring DockerMonitoringConfig `toml:"docker_monitoring" comment:"Cagent monitors all running docker containers and reports them for further processing to the Hub.\nYou can change the following settings."`

	MemMonitoring bool `toml:"mem_monitoring" comment:"\nTurn on or off parts of the monitoring.\nPresets of the operation_mode have precedence.\nWhat's disabled by the operation_mode can't be turned on here.\nBut it can still be turned off.\n\nTurn on/off the monitoring of memory"`

	CPUMonitoring bool `toml:"cpu_monitoring" comment:"Turn on/off any CPU related monitoring including the cpu_utilisation_analysis"`

	FSMonitoring bool `toml:"fs_monitoring" comment:"Turn on/off any disk- and filesystem-related monitoring like fill levels and iops"`

	NetMonitoring bool `toml:"net_monitoring" comment:"Turn on/off any network-related monitoring"`

	OnHTTP5xxRetries       int     `toml:"on_http_5xx_retries" comment:"Number of retries if server replies with a 5xx code"`
	OnHTTP5xxRetryInterval float64 `toml:"on_http_5xx_retry_interval" comment:"Interval in seconds between retries to contact server in case of a 5xx code"`
}

type ConfigDeprecated struct {
	WindowsUpdatesWatcherInterval int `toml:"windows_updates_watcher_interval" comment:""`
}

type CPUUtilisationAnalysisConfig struct {
	Threshold                      float64 `toml:"threshold" comment:"target value to start the analysis" json:"threshold"`
	Function                       string  `toml:"function" comment:"threshold compare function, possible values: 'lt', 'lte', 'gt', 'gte'" json:"function"`
	Metric                         string  `toml:"metric" commend:"possible values: 'user','system','idle','iowait'" json:"metric"`
	GatheringMode                  string  `toml:"gathering_mode" comment:"should be one of values of cpu_utilisation_gathering_mode" json:"gathering_mode"`
	ReportProcesses                int     `toml:"report_processes" comment:"number of processes to return" json:"report_processes"`
	TrailingProcessAnalysisMinutes int     `toml:"trailing_process_analysis_minutes" comment:"how much time analysis will continue to perform after the CPU utilisation returns to the normal value" json:"trailing_process_analysis_minutes"`
}

type StorCLIConfig struct {
	BinaryPath string `toml:"binary" comment:"Enable on Windows:\n  binary = 'C:\\Program Files\\storcli\\storcli64.exe'\nEnable on Linux:\n  binary = '/opt/storcli/sbin/storcli64'"`
}

type UpdatesMonitoringConfig struct {
	Enabled       bool   `toml:"enabled" comment:"Set 'false' to disable checking available updates"`
	FetchTimeout  uint32 `toml:"fetch_timeout" comment:"Maximum time the package manager is allowed to spend fetching available updates, ignored on windows"`
	CheckInterval uint32 `toml:"check_interval" comment:"Check for available updates every N seconds. Minimum is 300 seconds"`
}

type DockerMonitoringConfig struct {
	Enabled bool `toml:"enabled" comment:"Set 'false' to disable docker monitoring'"`
}

func (l *UpdatesMonitoringConfig) Validate() error {
	if l.FetchTimeout >= l.CheckInterval {
		return errors.New("fetch_timeout should be less than check_interval")
	}

	if l.CheckInterval < minSystemUpdatesCheckInterval {
		log.Warningf("system_updates_checks.check_interval is less than minimum(%d). It was set to %d", minSystemUpdatesCheckInterval, minSystemUpdatesCheckInterval)
		l.CheckInterval = minSystemUpdatesCheckInterval
	}

	return nil
}

type UpdatesConfig struct {
	Enabled       bool   `toml:"enabled" comment:"Set 'false' to disable self-updates"`
	URL           string `toml:"url" comment:"URL for updates feed"`
	CheckInterval uint32 `toml:"check_interval" comment:"Cagent will check for new versions every N seconds"`
}

func (u *UpdatesConfig) Validate() error {
	if u.CheckInterval < minSelfUpdatesCheckInterval {
		return fmt.Errorf("check_interval must be greater than %d seconds", minSelfUpdatesCheckInterval)
	}
	return nil
}

func (u *UpdatesConfig) GetCheckInterval() time.Duration {
	return time.Duration(int64(u.CheckInterval) * int64(time.Second))
}

type JobMonitoringConfig struct {
	SpoolDirPath string          `toml:"spool_dir" comment:"Path to spool dir"`
	RecordStdErr bool            `toml:"record_stderr" comment:"Record the last 4 KB of the error output. Default: true"`
	RecordStdOut bool            `toml:"record_stdout" comment:"Record the last 4 KB of the standard output. Default: false"`
	Severity     jobmon.Severity `toml:"severity" comment:"Failed jobs will be processed as alerts. Possible values alert, warning or none. Default: alert"`
}

func (j *JobMonitoringConfig) Validate() error {
	if len(j.SpoolDirPath) == 0 {
		return errors.New("spool_dir is empty")
	}

	if !filepath.IsAbs(j.SpoolDirPath) {
		return errors.New("spool_dir path must be absolute")
	}

	if !jobmon.IsValidJobMonitoringSeverity(j.Severity) {
		return fmt.Errorf("severity has invalid value. Must be one of %v", jobmon.ValidSeverities)
	}

	return nil
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
		DefaultCfgPath = "/etc/cagent/cagent.conf"
		defaultLogPath = "/var/log/cagent/cagent.log"
	}
}

func NewConfig() *Config {
	cfg := &Config{
		LogFile:                          defaultLogPath,
		OperationMode:                    OperationModeFull,
		Interval:                         90,
		HeartbeatInterval:                15,
		HubGzip:                          true,
		HubRequestTimeout:                30,
		CPULoadDataGather:                []string{"avg1"},
		CPUUtilTypes:                     []string{"user", "system", "idle", "iowait"},
		CPUUtilDataGather:                []string{"avg1"},
		FSTypeInclude:                    []string{"ext3", "ext4", "xfs", "jfs", "ntfs", "btrfs", "hfs", "apfs", "fat32", "smbfs", "nfs"},
		FSPathExclude:                    []string{},
		FSPathExcludeRecurse:             false,
		FSMetrics:                        []string{"free_B", "free_percent", "total_B", "read_B_per_s", "write_B_per_s", "read_ops_per_s", "write_ops_per_s"},
		FSIdentifyMountpointsByDevice:    true,
		NetMetrics:                       []string{"in_B_per_s", "out_B_per_s", "total_out_B_per_s", "total_in_B_per_s"},
		NetInterfaceExcludeDisconnected:  true,
		NetInterfaceExclude:              []string{},
		NetInterfaceExcludeRegex:         []string{"^vnet(.*)$", "^virbr(.*)$", "^vmnet(.*)$", "^vEthernet(.*)$"},
		NetInterfaceExcludeLoopback:      true,
		SystemFields:                     []string{"uname", "os_kernel", "os_family", "os_arch", "cpu_model", "fqdn", "memory_total_B"},
		HardwareInventory:                true,
		DiscoverAutostartingServicesOnly: true,
		CPUUtilisationAnalysis: CPUUtilisationAnalysisConfig{
			Threshold:                      10,
			Function:                       "lt",
			Metric:                         "idle",
			GatheringMode:                  "avg1",
			ReportProcesses:                5,
			TrailingProcessAnalysisMinutes: 5,
		},
		SMARTMonitoring:        false,
		TemperatureMonitoring:  true,
		SoftwareRAIDMonitoring: true,
		Logs: LogsFilesConfig{
			HubFile: "",
		},
		StorCLI: StorCLIConfig{
			BinaryPath: "",
		},
		JobMonitoring: JobMonitoringConfig{
			RecordStdErr: true,
			Severity:     jobmon.SeverityAlert,
			SpoolDirPath: "/var/lib/cagent/jobmon",
		},
		SystemUpdatesChecks: UpdatesMonitoringConfig{
			Enabled:       true,
			FetchTimeout:  30,
			CheckInterval: 14400,
		},
		ProcessMonitoring: processes.GetDefaultConfig(),
		Updates: UpdatesConfig{
			Enabled:       false,
			CheckInterval: 21600,
		},
		DockerMonitoring: DockerMonitoringConfig{Enabled: true},
		MemMonitoring:    true,
		CPUMonitoring:    true,
		FSMonitoring:     true,
		NetMonitoring:    true,

		OnHTTP5xxRetries:       4,
		OnHTTP5xxRetryInterval: 2.0,
	}

	cfg.MinValuableConfig = *(defaultMinValuableConfig())

	switch runtime.GOOS {
	case "windows":
		cfg.NetInterfaceExcludeRegex = append(cfg.NetInterfaceExcludeRegex, "Pseudo-Interface")
		cfg.CPULoadDataGather = []string{}
		cfg.CPUUtilTypes = []string{"user", "system", "idle"}
		cfg.VirtualMachinesStat = []string{"hyper-v"}
		cfg.JobMonitoring.SpoolDirPath = "C:\\ProgramData\\cagent\\jobmon"
		cfg.Updates.Enabled = true
		cfg.Updates.URL = SelfUpdatesFeedURL
	case "darwin":
		cfg.JobMonitoring.SpoolDirPath = "/usr/local/var/lib/cagent/jobmon"
	default:
		cfg.FSMetrics = append(cfg.FSMetrics, "inodes_used_percent")
	}

	return cfg
}

func NewMinimumConfig() *MinValuableConfig {
	cfg := defaultMinValuableConfig()

	cfg.applyEnv(false)

	if cfg.HubURL == "" {
		cfg.IOMode = IOModeFile
		if runtime.GOOS == "windows" {
			cfg.OutFile = "NUL"
		} else {
			cfg.OutFile = "/dev/null"
		}
	} else {
		cfg.IOMode = IOModeHTTP
	}

	return cfg
}

func defaultMinValuableConfig() *MinValuableConfig {
	return &MinValuableConfig{
		LogLevel: LogLevelError,
		IOMode:   IOModeHTTP,
	}
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

	cfgFile, err := os.Open(configFilePath)
	if err != nil {
		return err
	}

	_, err = toml.DecodeReader(cfgFile, cfg)
	if err != nil {
		return err
	}

	_, err = cfgFile.Seek(0, 0)
	if err != nil {
		return err
	}

	var deprecatedCfg ConfigDeprecated
	meta, err := toml.DecodeReader(cfgFile, &deprecatedCfg)
	if err != nil {
		return err
	}

	cfg.migrate(&deprecatedCfg, meta)

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

func (cfg *Config) GetParsedNetInterfaceMaxSpeed() (uint64, error) {
	v := cfg.NetInterfaceMaxSpeed
	if v == "" {
		return 0, nil
	}
	if len(v) < 2 {
		return 0, fmt.Errorf("can't parse")
	}

	valueStr, unit := v[0:len(v)-1], v[len(v)-1]
	value, err := strconv.ParseFloat(valueStr, 0)
	if err != nil {
		return 0, err
	}
	if value <= 0.0 {
		return 0, fmt.Errorf("should be > 0.0")
	}

	switch unit {
	case 'K':
		return uint64(value * 1000), nil
	case 'M':
		return uint64(value * 1000 * 1000), nil
	case 'G':
		return uint64(value * 1000 * 1000 * 1000), nil
	}

	return 0, fmt.Errorf("unsupported unit: %c", unit)
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

	if cfg.Interval < minIntervalValue {
		return fmt.Errorf("interval value must be >= %.1f", minIntervalValue)
	}

	if cfg.HeartbeatInterval < minHeartbeatIntervalValue {
		return fmt.Errorf("heartbeat value must be >= %.1f", minHeartbeatIntervalValue)
	}

	if !common.StrInSlice(cfg.OperationMode, operationModes) {
		return fmt.Errorf("invalid operation_mode supplied. Must be one of %v", operationModes)
	}

	_, err := cfg.GetParsedNetInterfaceMaxSpeed()
	if err != nil {
		return fmt.Errorf("invalid net_interface_max_speed value supplied: %s", err.Error())
	}

	if cfg.HubRequestTimeout < minHubRequestTimeout || cfg.HubRequestTimeout > maxHubRequestTimeout {
		return fmt.Errorf("hub_request_timeout must be between %d and %d", minHubRequestTimeout, maxHubRequestTimeout)
	}

	err = cfg.JobMonitoring.Validate()
	if err != nil {
		return fmt.Errorf("invalid [jobmon] config: %s", err.Error())
	}

	err = cfg.SystemUpdatesChecks.Validate()
	if err != nil {
		return fmt.Errorf("invalid [system_updates_checks] config: %s", err.Error())
	}

	err = cfg.MysqlMonitoring.Validate()
	if err != nil {
		return fmt.Errorf("invalid [mysql_monitoring] config: %s", err.Error())
	}

	err = cfg.Updates.Validate()
	if err != nil {
		return fmt.Errorf("invalid [updates] config: %s", err.Error())
	}

	if cfg.OnHTTP5xxRetries < 0 || cfg.OnHTTP5xxRetries > 5 {
		cfg.OnHTTP5xxRetries = 5
		log.Warn("on_http_5xx_retries value out of range (0-5). was reset to 5")
		return nil
	}

	if cfg.OnHTTP5xxRetryInterval < 1 || cfg.OnHTTP5xxRetryInterval > 3 {
		cfg.OnHTTP5xxRetryInterval = 3
		log.Warn("on_http_5xx_retry_interval value out of range (1-3). was reset to 3")
		return nil
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

func (cfg *Config) migrate(cfgDeprecated *ConfigDeprecated, metadata toml.MetaData) {
	// migrate windows_updates_watcher_interval into system_updates_checks.check_interval
	if runtime.GOOS == "windows" && metadata.IsDefined("windows_updates_watcher_interval") {
		if cfgDeprecated.WindowsUpdatesWatcherInterval <= 0 {
			cfg.SystemUpdatesChecks.Enabled = false
		} else {
			cfg.SystemUpdatesChecks.CheckInterval = uint32(cfgDeprecated.WindowsUpdatesWatcherInterval)
		}
	}
}
