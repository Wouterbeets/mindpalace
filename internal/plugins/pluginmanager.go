package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"

	"mindpalace/internal/plugingenerator"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// PluginManager handles loading and managing plugins
type PluginManager struct {
	plugins        []eventsourcing.Plugin
	eventProcessor *eventsourcing.EventProcessor
}

func NewPluginManager(ep *eventsourcing.EventProcessor) *PluginManager {
	pm := &PluginManager{
		eventProcessor: ep,
	}
	pm.LoadPlugins("plugins")
	return pm
}

// In PluginManager
func (pm *PluginManager) GetLLMPlugins() []eventsourcing.Plugin {
	var llmPlugins []eventsourcing.Plugin
	for _, plugin := range pm.plugins {
		if plugin.Type() == eventsourcing.LLMPlugin {
			llmPlugins = append(llmPlugins, plugin)
		}
	}
	return llmPlugins
}

func (pm *PluginManager) GetPlugin(name string) (eventsourcing.Plugin, error) {
	for _, plugin := range pm.plugins {
		if plugin.Name() == name {
			return plugin, nil
		}
	}
	return nil, fmt.Errorf("plugin '%s' not found", name)
}

func (pm *PluginManager) GetPluginByCommand(commandName string) (eventsourcing.Plugin, error) {
	for _, plugin := range pm.plugins {
		for name := range plugin.Commands() {
			if name == commandName {
				return plugin, nil
			}
		}
	}
	return nil, fmt.Errorf("plugin '%s' not found", commandName)
}

// / LoadPlugins finds, compiles if needed, and loads all plugins from the given directory
func (pm *PluginManager) LoadPlugins(pluginDir string) {
	logging.Debug("Starting to load plugins from directory: %s", pluginDir)

	pluginDirs, err := pm.discoverPluginDirectories(pluginDir)
	if err != nil {
		logging.Error("Error discovering plugin directories: %v", err)
		return
	}

	for _, dir := range pluginDirs {
		pluginName := filepath.Base(dir)
		soFile := filepath.Join(dir, pluginName+".so")

		shouldBuild, err := pm.shouldBuildPlugin(dir, soFile)
		if err != nil {
			logging.Error("Error checking if plugin needs building: %v", err)
			continue
		}

		if shouldBuild {
			if err := pm.buildPlugin(dir, soFile); err != nil {
				logging.Error("Failed to build plugin %s: %v", dir, err)
				continue
			}
		}

		// Attempt to load the plugin
		plugin, err := pm.loadPlugin(soFile)
		if err != nil {
			logging.Error("Failed to load plugin %s: %v", soFile, err)
			// Attempt to rebuild the plugin if loading failed
			if err := pm.buildPlugin(dir, soFile); err != nil {
				logging.Error("Failed to rebuild plugin %s: %v", dir, err)
				continue
			}
			// Try loading again after rebuilding
			plugin, err = pm.loadPlugin(soFile)
			if err != nil {
				logging.Error("Failed to load plugin after rebuild %s: %v", soFile, err)
				continue
			}
		}

		if plugin != nil {
			pm.plugins = append(pm.plugins, plugin)
			logging.Info("Successfully loaded plugin: %s", plugin.Name())
		}
	}

	logging.Info("Finished loading plugins, total loaded: %d", len(pm.plugins))
	commands := pm.RegisterCommands()
	for name, handler := range commands {
		logging.Debug("registering commands en eventprocessor after initial plugin loading: %s", name)
		pm.eventProcessor.RegisterCommand(name, handler)
	}
}

// discoverPluginDirectories finds all directories containing plugin.go files
func (pm *PluginManager) discoverPluginDirectories(rootDir string) ([]string, error) {
	var pluginDirs []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// We're only interested in directories at this point
		if !info.IsDir() {
			return nil
		}

		// Check if this directory contains a plugin.go file
		goFile := filepath.Join(path, "plugin.go")
		if _, err := os.Stat(goFile); err == nil {
			pluginDirs = append(pluginDirs, path)
			logging.Debug("Found plugin directory: %s", path)
		}

		return nil
	})

	return pluginDirs, err
}

// shouldBuildPlugin checks if a plugin should be built based on file existence or modification times
func (pm *PluginManager) shouldBuildPlugin(dir, soFile string) (bool, error) {
	// Glob all .go files in the directory
	goFiles, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return false, fmt.Errorf("error globbing .go files: %w", err)
	}
	if len(goFiles) == 0 {
		return false, fmt.Errorf("no .go files found in directory: %s", dir)
	}

	// Check if the SO file exists
	soInfo, err := os.Stat(soFile)
	if os.IsNotExist(err) {
		// SO doesn't exist, we should build
		logging.Debug("Plugin SO file doesn't exist, will build: %s", soFile)
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("SO file check error: %w", err)
	}

	// Check if any GO file is newer than SO file
	for _, goFile := range goFiles {
		goInfo, err := os.Stat(goFile)
		if err != nil {
			return false, fmt.Errorf("source file error for %s: %w", goFile, err)
		}
		if goInfo.ModTime().After(soInfo.ModTime()) {
			logging.Debug("Plugin source is newer than SO, will rebuild: %s", goFile)
			return true, nil
		}
	}

	// SO file exists and is up to date
	logging.Debug("Plugin SO file is up to date: %s", soFile)
	return false, nil
}

// buildPlugin compiles a plugin from the given source to the given output
func (pm *PluginManager) buildPlugin(dir, soFile string) error {
	logging.Debug("Building plugin from %s to %s", dir, soFile)

	// Glob all .go files in the directory
	goFiles, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return fmt.Errorf("error globbing .go files: %w", err)
	}
	if len(goFiles) == 0 {
		return fmt.Errorf("no .go files found in directory: %s", dir)
	}

	// If SO file already exists, remove it first to avoid any issues
	if _, err := os.Stat(soFile); err == nil {
		if err := os.Remove(soFile); err != nil {
			return fmt.Errorf("failed to remove existing SO file: %w", err)
		}
	}

	// Prepare arguments for go build
	args := []string{"build", "-buildmode=plugin", "-o", soFile}
	args = append(args, goFiles...)

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build command failed: %w", err)
	}

	// Verify the file was actually created
	if _, err := os.Stat(soFile); os.IsNotExist(err) {
		return fmt.Errorf("build appeared to succeed but file wasn't created")
	}

	logging.Debug("Successfully built plugin: %s", soFile)
	return nil
}

// loadPlugin loads a plugin from the given SO file
func (pm *PluginManager) loadPlugin(soFile string) (eventsourcing.Plugin, error) {
	logging.Debug("Loading plugin from: %s", soFile)

	plug, err := plugin.Open(soFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	sym, err := plug.Lookup("NewPlugin")
	if err != nil {
		return nil, fmt.Errorf("plugin does not export NewPlugin: %w", err)
	}

	newPlugin, ok := sym.(func() eventsourcing.Plugin)
	if !ok {
		return nil, fmt.Errorf("NewPlugin is not of the correct type")
	}

	pluginInstance := newPlugin()
	if pluginInstance == nil {
		return nil, fmt.Errorf("NewPlugin returned nil")
	}

	return pluginInstance, nil
}

func (pm *PluginManager) RegisterCommands() map[string]eventsourcing.CommandHandler {
	commands := make(map[string]eventsourcing.CommandHandler)
	for _, p := range pm.plugins {
		for name, handler := range p.Commands() {
			if _, exists := commands[name]; exists {
				logging.Debug("Command %s already registered", name)
				continue
			}
			commands[name] = handler
		}
	}
	return commands
}

// LoadNewPlugin loads and registers a new plugin from the given path
func (pm *PluginManager) LoadNewPlugin(pluginPath string) error {
	plugin, err := pm.loadPlugin(pluginPath)
	if err != nil {
		// If loading fails, attempt to rebuild from source if we can find it
		dir := filepath.Dir(pluginPath)
		if _, statErr := os.Stat(filepath.Join(dir, "plugin.go")); statErr == nil {
			logging.Info("Attempting to rebuild plugin from source: %s", dir)
			if buildErr := pm.buildPlugin(dir, pluginPath); buildErr != nil {
				return fmt.Errorf("failed to rebuild plugin: %w", buildErr)
			}
			// Try loading again after rebuild
			plugin, err = pm.loadPlugin(pluginPath)
			if err != nil {
				return fmt.Errorf("failed to load plugin after rebuild: %w", err)
			}
		} else {
			return fmt.Errorf("failed to load plugin and no source found for rebuild: %w", err)
		}
	}

	pm.plugins = append(pm.plugins, plugin)
	commands := pm.RegisterCommands()
	for name, handler := range commands {
		pm.eventProcessor.RegisterCommand(name, handler)
	}
	return nil
}

// GenerateAndLoadPlugin generates a new plugin based on requirements and loads it
func (pm *PluginManager) GenerateAndLoadPlugin() error {
	pg := plugingenerator.NewPluginGenerator()
	req, err := pg.ConductInterview()
	if err != nil {
		return fmt.Errorf("failed to conduct interview: %v", err)
	}

	if err := pg.GeneratePlugin(req); err != nil {
		return fmt.Errorf("failed to generate plugin: %v", err)
	}

	// Build and load the plugin
	pluginDir := filepath.Join("plugins", req.Name)
	soFile := filepath.Join(pluginDir, req.Name+".so")
	if err := pm.buildPlugin(pluginDir, soFile); err != nil {
		return fmt.Errorf("failed to build generated plugin: %v", err)
	}

	return pm.LoadNewPlugin(soFile)
}
