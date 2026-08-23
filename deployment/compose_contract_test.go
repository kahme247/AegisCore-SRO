package deployment

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeService struct {
	Volumes     []string                     `yaml:"volumes"`
	Ports       []string                     `yaml:"ports"`
	Expose      []string                     `yaml:"expose"`
	Profiles    []string                     `yaml:"profiles"`
	Environment map[string]string            `yaml:"environment"`
	Image       string                       `yaml:"image"`
	Build       composeBuild                 `yaml:"build"`
	DependsOn   map[string]composeDependency `yaml:"depends_on"`
}

func TestComposeKeepsSMCAndAgentManagementPrivateAndOptIn(t *testing.T) {
	data, err := os.ReadFile("../docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	var compose composeContract
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatalf("parse docker-compose.yml: %v", err)
	}
	agent := compose.Services["agent-server"]
	for _, published := range agent.Ports {
		if published == "15884" || published == "15884:15884" {
			t.Fatalf("Agent management port is host-published: %v", agent.Ports)
		}
	}
	foundManagementExpose := false
	for _, exposed := range agent.Expose {
		if exposed == "15884" {
			foundManagementExpose = true
		}
	}
	if !foundManagementExpose {
		t.Fatalf("Agent private exposes = %v, want 15884", agent.Expose)
	}
	if agent.Environment["AGENT_MANAGEMENT_ENABLED"] != "${AGENT_MANAGEMENT_ENABLED:-0}" {
		t.Fatal("Agent management must default disabled")
	}

	smc := compose.Services["smc-server"]
	if len(smc.Profiles) != 1 || smc.Profiles[0] != "smc" {
		t.Fatalf("SMC profiles = %v, want opt-in smc profile", smc.Profiles)
	}
	if len(smc.Ports) != 1 || smc.Ports[0] != "127.0.0.1:${SMC_PUBLIC_PORT:-15900}:15900" {
		t.Fatalf("SMC ports = %v, want loopback-only publication", smc.Ports)
	}
	if smc.Environment["SMC_AGENT_MANAGEMENT_ENABLED"] != "${SMC_AGENT_MANAGEMENT_ENABLED:-0}" {
		t.Fatal("SMC Agent management client must default disabled")
	}
}

type composeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

type composeDependency struct {
	Condition string `yaml:"condition"`
}

type composeContract struct {
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes"`
}

func TestComposePersistsMySQLAndWaitsForSchemaHealth(t *testing.T) {
	data, err := os.ReadFile("../docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	var compose composeContract
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatalf("parse docker-compose.yml: %v", err)
	}
	if _, exists := compose.Volumes["mysql-data"]; !exists {
		t.Fatal("Compose does not declare the stable mysql-data volume")
	}
	database := compose.Services["db"]
	if database.Image != "aegiscore-db:8.4.10" {
		t.Fatalf("database image = %q, want pinned MySQL 8.4 application image", database.Image)
	}
	if database.Build.Context != "./db" || database.Build.Dockerfile != "Dockerfile" {
		t.Fatalf("database build = %+v, want repository db/Dockerfile", database.Build)
	}
	if len(database.Ports) != 1 || database.Ports[0] != "127.0.0.1:3306:3306" {
		t.Fatalf("database ports = %v, want loopback-only classic MySQL", database.Ports)
	}
	foundDataMount := false
	for _, volume := range database.Volumes {
		if volume == "mysql-data:/var/lib/mysql" {
			foundDataMount = true
			break
		}
	}
	if !foundDataMount {
		t.Fatal("database service does not mount mysql-data at /var/lib/mysql")
	}
	if got := database.Environment["MYSQL_PASSWORD"]; got != "${MYSQL_PASSWORD:?Set MYSQL_PASSWORD to the sro database password}" {
		t.Fatalf("database MYSQL_PASSWORD = %q, want required interpolation", got)
	}
	for _, serviceName := range []string{"agent-server", "gateway-server"} {
		environment := compose.Services[serviceName].Environment
		if environment["DB_ACCOUNT_PW"] == "" || environment["DB_SHARD_PW"] == "" {
			t.Fatalf("%s does not receive both required database passwords", serviceName)
		}
	}
	for _, serviceName := range []string{"agent-server", "gateway-server"} {
		dependency, exists := compose.Services[serviceName].DependsOn["db"]
		if !exists || dependency.Condition != "service_healthy" {
			t.Fatalf("%s database dependency = %+v, want service_healthy", serviceName, dependency)
		}
	}
}
