package deployment

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeService struct {
	Volumes   []string                     `yaml:"volumes"`
	DependsOn map[string]composeDependency `yaml:"depends_on"`
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
