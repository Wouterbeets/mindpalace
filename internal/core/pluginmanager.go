package core

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"

	"mindpalace/pkg/eventsourcing"
)

// PluginManager handles loading and managing plugins
type PluginManager struct {
	plugins       []eventsourcing.Plugin
	eventHandlers map[string][]eventsourcing.EventHandler
}

func NewPluginManager() *PluginManager {
	return &PluginManager{
		eventHandlers: make(map[string][]eventsourcing.EventHandler),
	}
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
func (pm *PluginManager) LoadPlugins(pluginDir string, ep *eventsourcing.EventProcessor) {
	// Log the directory we're starting to scan
	log.Printf("Starting to load plugins from directory: %s", pluginDir)
	err := filepath.Walk(pluginDir, func(path string, info os.FileInfo, err error) error {
		// Log any errors encountered while traversing the directory tree
		if err != nil {
			log.Printf("Error walking path %s: %v", path, err)
			return nil // Continue despite errors to avoid stopping the walk
		}
		// Log the current file or directory being checked
		log.Printf("Checking path: %s, IsDir: %t, Ext: %s", path, info.IsDir(), filepath.Ext(info.Name()))

		// Skip if it's a directory or not an .so file
		if info.IsDir() || filepath.Ext(info.Name()) != ".so" {
			// If it's a directory, check for a .go file and build if no .so exists
			if info.IsDir() {
				goFile := filepath.Join(path, "plugin.go")
				soFile := filepath.Join(path, info.Name()+".so")
				if _, err := os.Stat(goFile); err == nil { // .go file exists
					if _, err := os.Stat(soFile); os.IsNotExist(err) { // .so file doesn't exist
						log.Printf("No .so file found for %s, attempting to build from %s", path, goFile)
						cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", soFile, goFile)
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						if err := cmd.Run(); err != nil {
							log.Printf("Failed to build plugin %s: %v", goFile, err)
							return nil
						}
						log.Printf("Successfully built %s into %s", goFile, soFile)
						path = soFile // Update path to the newly built .so file
					} else if err != nil && !os.IsNotExist(err) {
						log.Printf("Error checking .so file %s: %v", soFile, err)
						return nil
					}
				}
			}
			// Log skipping directories or non-.so files
			if filepath.Ext(info.Name()) != ".so" {
				log.Printf("Skipping %s: not an .so file", path)
				return nil
			}
			return nil
		}

		// Log attempt to open the .so file
		log.Printf("Attempting to open plugin file: %s", path)
		plug, err := plugin.Open(path)
		if err != nil {
			log.Printf("Failed to load plugin %s: %v", path, err)
			return nil // Continue to next file if opening fails
		}

		// Log attempt to lookup the NewPlugin symbol
		log.Printf("Looking up 'NewPlugin' in %s", path)
		sym, err := plug.Lookup("NewPlugin")
		if err != nil {
			log.Printf("Plugin %s does not export NewPlugin: %v", path, err)
			return nil // Continue if symbol lookup fails
		}

		// Log type assertion attempt
		log.Printf("Asserting NewPlugin type for %s", path)
		newPlugin, ok := sym.(func() eventsourcing.Plugin)
		if !ok {
			log.Printf("NewPlugin in %s is not a function returning Plugin", path)
			return nil // Continue if type assertion fails
		}

		// Log successful plugin addition
		log.Printf("Successfully loaded plugin from %s, appending to pm.plugins", path)
		pm.plugins = append(pm.plugins, newPlugin())
		return nil
	})
	if err != nil {
		// Log any overall error from filepath.Walk
		log.Printf("Failed to walk plugin directory %s: %v", pluginDir, err)
	}
	for _, plugin := range pm.plugins {
		for eventType, handler := range plugin.EventHandlers() {
			ep.RegisterEventHandler(eventType, handler)
		}
	}
	// Log the final state of loaded plugins
	log.Printf("Finished loading plugins, total loaded: %d", len(pm.plugins))
}

func (pm *PluginManager) RegisterCommands() (map[string]eventsourcing.CommandHandler, map[string][]eventsourcing.EventHandler) {
	commands := make(map[string]eventsourcing.CommandHandler)
	for _, p := range pm.plugins {
		for name, handler := range p.Commands() {
			if _, exists := commands[name]; exists {
				log.Printf("Command %s already registered", name)
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

// ProcessEvent processes an event by invoking all registered handlers for its type
func (pm *PluginManager) ProcessEvent(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) []eventsourcing.Event {
	var newEvents []eventsourcing.Event
	eventType := event.Type()
	handlers, exists := pm.eventHandlers[eventType]
	if !exists {
		log.Printf("No handlers registered for event type: %s", eventType)
		return nil
	}
	for _, handler := range handlers {
		events, err := handler(event, state, commands)
		if err != nil {
			log.Printf("Error processing event %s with handler: %v", eventType, err)
			continue
		}
		newEvents = append(newEvents, events...)
	}
	return newEvents
}
