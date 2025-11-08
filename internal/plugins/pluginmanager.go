package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"sync"
	"time"

	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/plugingenerator"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"mindpalace/pkg/modellib"
)

// PluginNames tracks loaded plugin names for dynamic zoning
var PluginNames []string

type pluginEntry struct {
	plugin    eventsourcing.Plugin
	metadata  eventsourcing.PluginMetadata
	telemetry eventsourcing.PluginTelemetry
}

// LLMClientConsumer allows plugins to receive the runtime LLM client for autonomous workflows.
type LLMClientConsumer interface {
	SetLLMClient(llm llmprocessor.LLMClient)
}

// PluginManager handles loading and managing plugins
type PluginManager struct {
	mu             sync.RWMutex
	plugins        map[string]*pluginEntry
	orderedNames   []string
	eventProcessor *eventsourcing.EventProcessor
	modelCatalog   *modellib.Catalog
	llmClient      llmprocessor.LLMClient
}

func NewPluginManager(ep *eventsourcing.EventProcessor, catalog *modellib.Catalog) *PluginManager {
	pm := &PluginManager{
		plugins:        make(map[string]*pluginEntry),
		eventProcessor: ep,
		modelCatalog:   catalog,
	}
	pm.LoadPlugins("plugins")
	return pm
}

// In PluginManager
func (pm *PluginManager) GetLLMPlugins() []eventsourcing.Plugin {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	llmPlugins := make([]eventsourcing.Plugin, 0, len(pm.plugins))
	for _, name := range pm.orderedNames {
		entry, exists := pm.plugins[name]
		if !exists {
			continue
		}
		if entry.plugin.Type() == eventsourcing.LLMPlugin {
			llmPlugins = append(llmPlugins, entry.plugin)
		}
	}
	return llmPlugins
}

func (pm *PluginManager) GetPlugin(name string) (eventsourcing.Plugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if entry, ok := pm.plugins[name]; ok {
		return entry.plugin, nil
	}
	return nil, fmt.Errorf("plugin '%s' not found", name)
}

func (pm *PluginManager) GetPluginByCommand(commandName string) (eventsourcing.Plugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, name := range pm.orderedNames {
		entry, exists := pm.plugins[name]
		if !exists {
			continue
		}
		for cmd := range entry.plugin.Commands() {
			if cmd == commandName {
				return entry.plugin, nil
			}
		}
	}
	return nil, fmt.Errorf("plugin '%s' not found", commandName)
}

// / LoadPlugins finds, compiles if needed, and loads all plugins from the given directory
func (pm *PluginManager) LoadPlugins(pluginDir string) {
	logging.Debug("Starting to load plugins from directory: %s", pluginDir)
	// Clear PluginNames to avoid duplicates on reload
	PluginNames = []string{}
	pm.mu.Lock()
	pm.plugins = make(map[string]*pluginEntry)
	pm.orderedNames = pm.orderedNames[:0]
	pm.mu.Unlock()

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
			continue
		}

		if plugin != nil {
			if consumer, ok := plugin.(modellib.CatalogConsumer); ok && pm.modelCatalog != nil {
				consumer.SetModelCatalog(pm.modelCatalog)
			}
			metadata := eventsourcing.DefaultPluginMetadata(plugin.Name())
			if provider, ok := plugin.(eventsourcing.PluginMetadataProvider); ok {
				metadata = provider.Metadata()
			}
			if metadata.Name == "" {
				metadata.Name = plugin.Name()
			}
			if metadata.DefaultTimeout <= 0 {
				metadata.DefaultTimeout = 30 * time.Second
			}

			pm.mu.Lock()
			pm.plugins[plugin.Name()] = &pluginEntry{
				plugin:    plugin,
				metadata:  metadata,
				telemetry: eventsourcing.PluginTelemetry{},
			}
			pm.orderedNames = append(pm.orderedNames, plugin.Name())
			pm.mu.Unlock()

			if pm.llmClient != nil {
				if consumer, ok := plugin.(LLMClientConsumer); ok {
					consumer.SetLLMClient(pm.llmClient)
				}
			}

			PluginNames = append(PluginNames, plugin.Name())
			logging.Info("Successfully loaded plugin: %s", plugin.Name())
		}
	}

	pm.mu.RLock()
	totalPlugins := len(pm.plugins)
	pm.mu.RUnlock()

	logging.Info("Finished loading plugins, total loaded: %d", totalPlugins)
	commands := pm.RegisterCommands()
	for name, handler := range commands {
		logging.Debug("registering commands en eventprocessor after initial plugin loading: %s", name)
		pm.eventProcessor.RegisterCommand(name, handler)
	}
}

// InjectLLMClient hands the live LLM runtime to any interested plugins.
func (pm *PluginManager) InjectLLMClient(client llmprocessor.LLMClient) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.llmClient = client
	for _, entry := range pm.plugins {
		if consumer, ok := entry.plugin.(LLMClientConsumer); ok {
			consumer.SetLLMClient(client)
		}
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

	// Set environment variables needed for CGO builds (same as Makefile)
	cmd.Env = append(os.Environ(),
		"PKG_CONFIG_PATH="+os.Getenv("PKG_CONFIG_PATH"),
		"CGO_LDFLAGS="+os.Getenv("CGO_LDFLAGS"),
	)

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
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, name := range pm.orderedNames {
		entry := pm.plugins[name]
		for cmdName, handler := range entry.plugin.Commands() {
			if _, exists := commands[cmdName]; exists {
				logging.Debug("Command %s already registered", cmdName)
				continue
			}
			commands[cmdName] = handler
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

	if consumer, ok := plugin.(modellib.CatalogConsumer); ok && pm.modelCatalog != nil {
		consumer.SetModelCatalog(pm.modelCatalog)
	}

	metadata := eventsourcing.DefaultPluginMetadata(plugin.Name())
	if provider, ok := plugin.(eventsourcing.PluginMetadataProvider); ok {
		metadata = provider.Metadata()
	}
	if metadata.Name == "" {
		metadata.Name = plugin.Name()
	}
	if metadata.DefaultTimeout <= 0 {
		metadata.DefaultTimeout = 30 * time.Second
	}

	pm.mu.Lock()
	pm.plugins[plugin.Name()] = &pluginEntry{
		plugin:    plugin,
		metadata:  metadata,
		telemetry: eventsourcing.PluginTelemetry{},
	}
	pm.orderedNames = append(pm.orderedNames, plugin.Name())
	pm.mu.Unlock()

	PluginNames = append(PluginNames, plugin.Name())

	commands := pm.RegisterCommands()
	for name, handler := range commands {
		pm.eventProcessor.RegisterCommand(name, handler)
	}
	return nil
}

// SetModelCatalog injects or updates the shared catalog reference and informs catalog-aware plugins.
func (pm *PluginManager) SetModelCatalog(catalog *modellib.Catalog) {
	pm.mu.Lock()
	pm.modelCatalog = catalog
	entries := make([]*pluginEntry, 0, len(pm.plugins))
	for _, entry := range pm.plugins {
		entries = append(entries, entry)
	}
	pm.mu.Unlock()

	if catalog == nil {
		return
	}
	for _, entry := range entries {
		if consumer, ok := entry.plugin.(modellib.CatalogConsumer); ok {
			consumer.SetModelCatalog(catalog)
		}
	}
}

// RecordInvocation updates telemetry for the given plugin and returns the merged snapshot.
func (pm *PluginManager) RecordInvocation(name string, result eventsourcing.PluginInvocationResult) eventsourcing.PluginTelemetry {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	entry, exists := pm.plugins[name]
	if !exists {
		return eventsourcing.PluginTelemetry{}
	}
	entry.telemetry = entry.telemetry.Merge(result)
	return entry.telemetry
}

// PluginMetadataSnapshot returns a lightweight metadata snapshot for a plugin.
func (pm *PluginManager) PluginMetadataSnapshot(name string) (eventsourcing.PluginMetadataSnapshot, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	entry, exists := pm.plugins[name]
	if !exists {
		return eventsourcing.PluginMetadataSnapshot{}, false
	}
	return entry.metadata.Snapshot(), true
}

// PluginDefaultTimeout returns the configured timeout for the plugin or the system fallback.
func (pm *PluginManager) PluginDefaultTimeout(name string, fallback time.Duration) time.Duration {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	entry, exists := pm.plugins[name]
	if !exists {
		return fallback
	}
	if entry.metadata.DefaultTimeout <= 0 {
		return fallback
	}
	return entry.metadata.DefaultTimeout
}

// PluginSnapshots returns metadata + telemetry snapshots for all loaded plugins.
func (pm *PluginManager) PluginSnapshots() []eventsourcing.PluginSnapshot {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	snapshots := make([]eventsourcing.PluginSnapshot, 0, len(pm.orderedNames))
	for _, name := range pm.orderedNames {
		entry := pm.plugins[name]
		snapshots = append(snapshots, eventsourcing.PluginSnapshot{
			Metadata:  entry.metadata.Snapshot(),
			Telemetry: entry.telemetry,
		})
	}
	return snapshots
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
