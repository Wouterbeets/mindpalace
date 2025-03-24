package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"

	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// PluginManager handles loading and managing plugins
type PluginManager struct {
	plugins        []eventsourcing.Plugin
	eventHandlers  map[string][]eventsourcing.EventHandler
	eventProcessor *eventsourcing.EventProcessor
}

func NewPluginManager(ep *eventsourcing.EventProcessor) *PluginManager {
	return &PluginManager{
		eventHandlers:  make(map[string][]eventsourcing.EventHandler),
		eventProcessor: ep,
	}
}

// In PluginManager
func (pm *PluginManager) GetLLMPlugins() []eventsourcing.Plugin {
	var llmPlugins []eventsourcing.Plugin
	for _, plugin := range pm.plugins {
		logging.Trace("Checking plugin %s for LLM capability, schemas: %v", plugin.Name(), plugin.Schemas())
		if plugin.Type() == eventsourcing.LLMPlugin {
			llmPlugins = append(llmPlugins, plugin)
		}
	}
	return llmPlugins
}

// LoadPlugins finds, compiles if needed, and loads all plugins from the given directory
func (pm *PluginManager) LoadPlugins(pluginDir string, ep *eventsourcing.EventProcessor) {
	logging.Debug("Starting to load plugins from directory: %s", pluginDir)

	pluginDirs, err := pm.discoverPluginDirectories(pluginDir)
	if err != nil {
		logging.Error("Error discovering plugin directories: %v", err)
		return
	}

	for _, dir := range pluginDirs {
		pluginName := filepath.Base(dir)
		goFile := filepath.Join(dir, "plugin.go")
		soFile := filepath.Join(dir, pluginName+".so")

		shouldBuild, err := pm.shouldBuildPlugin(goFile, soFile)
		if err != nil {
			logging.Error("Error checking if plugin needs building: %v", err)
			continue
		}

		if shouldBuild {
			if err := pm.buildPlugin(goFile, soFile); err != nil {
				logging.Error("Failed to build plugin %s: %v", goFile, err)
				continue
			}
		}

		// Attempt to load the plugin
		plugin, err := pm.loadPlugin(soFile)
		if err != nil {
			logging.Error("Failed to load plugin %s: %v", soFile, err)
			// Attempt to rebuild the plugin if loading failed
			if err := pm.buildPlugin(goFile, soFile); err != nil {
				logging.Error("Failed to rebuild plugin %s: %v", goFile, err)
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

			for eventType, handler := range plugin.EventHandlers() {
				ep.RegisterEventHandler(eventType, handler)
			}
		}
	}

	logging.Info("Finished loading plugins, total loaded: %d", len(pm.plugins))
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
func (pm *PluginManager) shouldBuildPlugin(goFile, soFile string) (bool, error) {
	// Check if the source file exists
	goInfo, err := os.Stat(goFile)
	if err != nil {
		return false, fmt.Errorf("source file error: %w", err)
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

	// Both files exist, check if GO file is newer than SO file
	if goInfo.ModTime().After(soInfo.ModTime()) {
		logging.Debug("Plugin source is newer than SO, will rebuild: %s", goFile)
		return true, nil
	}

	// SO file exists and is up to date
	logging.Debug("Plugin SO file is up to date: %s", soFile)
	return false, nil
}

// buildPlugin compiles a plugin from the given source to the given output
func (pm *PluginManager) buildPlugin(goFile, soFile string) error {
	logging.Debug("Building plugin from %s to %s", goFile, soFile)

	// If SO file already exists, remove it first to avoid any issues
	if _, err := os.Stat(soFile); err == nil {
		if err := os.Remove(soFile); err != nil {
			return fmt.Errorf("failed to remove existing SO file: %w", err)
		}
	}

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", soFile, goFile)
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

func (pm *PluginManager) RegisterCommands() (map[string]eventsourcing.CommandHandler, map[string][]eventsourcing.EventHandler) {
	commands := make(map[string]eventsourcing.CommandHandler)
	for _, p := range pm.plugins {
		for name, handler := range p.Commands() {
			if _, exists := commands[name]; exists {
				logging.Debug("Command %s already registered", name)
				continue
			}
			commands[name] = handler
		}
		for eventType, handler := range p.EventHandlers() {
			pm.eventHandlers[eventType] = append(pm.eventHandlers[eventType], handler)
		}
	}
	return commands, pm.eventHandlers
}

// LoadNewPlugin loads and registers a new plugin from the given path
func (pm *PluginManager) LoadNewPlugin(pluginPath string) error {
	plugin, err := pm.loadPlugin(pluginPath)
	if err != nil {
		// If loading fails, attempt to rebuild from source if we can find it
		goFile := filepath.Join(filepath.Dir(pluginPath), "plugin.go")
		if _, statErr := os.Stat(goFile); statErr == nil {
			logging.Info("Attempting to rebuild plugin from source: %s", goFile)
			if buildErr := pm.buildPlugin(goFile, pluginPath); buildErr != nil {
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
	commands, handlers := pm.RegisterCommands()
	for name, handler := range commands {
		pm.eventProcessor.RegisterCommand(name, handler)
	}
	for eventType, handlerList := range handlers {
		for _, handler := range handlerList {
			pm.eventProcessor.RegisterEventHandler(eventType, handler)
		}
	}
	return nil
}
