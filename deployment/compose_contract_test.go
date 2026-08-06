package deployment

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeService struct {
	Volumes   []string                     `yaml:"volumes"`
	Ports     []string                     `yaml:"ports"`
	Image     string                       `yaml:"image"`
	Build     composeBuild                 `yaml:"build"`
	DependsOn map[string]composeDependency `yaml:"depends_on"`
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
	if database.Image != "gosro-db:8.4.10" {
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
	for _, serviceName := range []string{"agent-server", "gateway-server"} {
		dependency, exists := compose.Services[serviceName].DependsOn["db"]
		if !exists || dependency.Condition != "service_healthy" {
			t.Fatalf("%s database dependency = %+v, want service_healthy", serviceName, dependency)
		}
	}
}
