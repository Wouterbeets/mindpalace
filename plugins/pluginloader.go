package plugins

import (
	"fmt"
	"log"
	"mindpalace/internal/eventsourcing/interfaces"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"reflect"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Loader struct {
	plugins    map[string]*plugin.Plugin
	mutex      sync.Mutex
	watcher    *fsnotify.Watcher
	pluginPath string
}

// NewLoader initializes a new plugin loader
func NewLoader(path string) (*Loader, error) {
	l := &Loader{
		plugins: make(map[string]*plugin.Plugin),
	}
	err := l.LoadPlugins(path)
	if err != nil {
		return nil, fmt.Errorf("unable to load plugins for path: %s, error: %w", path, err)
	}
	return l, nil
}

// LoadPlugins loads the plugins from the provided path and watches for changes.
func (l *Loader) LoadPlugins(path string) error {
	l.pluginPath = path

	var err error
	l.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("error creating file watcher: %w", err)
	}

	go l.watchPlugins()

	// Add all directories to the watcher recursively
	err = filepath.WalkDir(path, func(currPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			err := l.watcher.Add(currPath)
			if err != nil {
				log.Printf("Warning: error adding directory to watcher: %v", err)
			}
		}
		return nil
	})

	if err != nil {
		_ = l.watcher.Close() // Attempt to close watcher on error to release resources
		return fmt.Errorf("error adding paths to watcher: %w", err)
	}

	// Initial loading of plugins
	loadErr := l.buildAndLoadAllPlugins()
	if loadErr != nil {
		_ = l.watcher.Close() // Cleanup resources on failure
		return fmt.Errorf("error loading initial plugins: %w", loadErr)
	}

	return nil
}

// buildAndLoadAllPlugins builds and loads all plugins in the pluginPath, recursively.
func (l *Loader) buildAndLoadAllPlugins() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	err := filepath.WalkDir(l.pluginPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the pluginloader file itself
		if filepath.Base(path) == "pluginloader.go" {
			return nil
		}

		if !d.IsDir() && filepath.Ext(d.Name()) == ".go" {
			// Build the Go file into a plugin
			l.buildPlugin(path)
		}
		if !d.IsDir() && filepath.Ext(d.Name()) == ".so" {
			if _, exists := l.plugins[path]; !exists {
				err := l.loadPlugin(path)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

// loadPlugin loads an individual plugin and stores it in the plugins map
func (l *Loader) loadPlugin(pluginFilePath string) error {
	p, err := plugin.Open(pluginFilePath)
	if err != nil {
		log.Printf("Error loading plugin %s: %v", pluginFilePath, err)
		return err
	}

	l.plugins[pluginFilePath] = p
	log.Printf("Plugin %s loaded successfully", pluginFilePath)
	return nil
}

// unloadPlugin removes a plugin from the plugins map
func (l *Loader) unloadPlugin(pluginFilePath string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if _, exists := l.plugins[pluginFilePath]; exists {
		delete(l.plugins, pluginFilePath)
		log.Printf("Plugin %s unloaded successfully", pluginFilePath)
	}
}

// watchPlugins watches for changes in the plugin directory and handles the events
func (l *Loader) watchPlugins() {
	for {
		select {
		case event, ok := <-l.watcher.Events:
			if !ok {
				return
			}

			l.handleEvent(event)

			// If a new directory is created, add it to the watcher
			if event.Op&fsnotify.Create == fsnotify.Create {
				fileInfo, err := os.Stat(event.Name)
				if err == nil && fileInfo.IsDir() {
					err = l.watcher.Add(event.Name)
					if err != nil {
						log.Printf("Error adding new directory to watcher: %v", err)
					}
				}
			}

		case err, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// handleEvent processes a single fsnotify event
func (l *Loader) handleEvent(event fsnotify.Event) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	pluginFilePath := event.Name

	// Skip the pluginloader file itself
	if filepath.Base(pluginFilePath) == "pluginloader.go" {
		return
	}

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		if filepath.Ext(pluginFilePath) == ".go" {
			l.buildPlugin(pluginFilePath)
		} else if filepath.Ext(pluginFilePath) == ".so" {
			l.loadPlugin(pluginFilePath)
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		l.unloadPlugin(pluginFilePath)

	case event.Op&fsnotify.Write == fsnotify.Write:
		if filepath.Ext(pluginFilePath) == ".go" {
			l.buildPlugin(pluginFilePath)
		} else if filepath.Ext(pluginFilePath) == ".so" {
			l.unloadPlugin(pluginFilePath)
			l.loadPlugin(pluginFilePath)
		}
	}
}

// buildPlugin compiles a Go source file into a .so plugin
func (l *Loader) buildPlugin(goFilePath string) {
	soFilePath := goFilePath[:len(goFilePath)-3] + ".so"
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", soFilePath, goFilePath)
	err := cmd.Run()
	if err != nil {
		log.Printf("Error building plugin %s: %v", goFilePath, err)
		return
	}
	log.Printf("Plugin %s built successfully", soFilePath)
}

// Plugins returns the currently loaded plugins
func (l *Loader) Plugins() map[string]*plugin.Plugin {
	return l.plugins
}

// Close releases the resources used by the Loader, including the file watcher.
func (l *Loader) Close() {
	if l.watcher != nil {
		l.watcher.Close()
	}
}

type CommandCreator struct {
	CommandType reflect.Type
}

func (cc CommandCreator) Create(creationParams map[string]interface{}) (interfaces.Command, error) {
	// Ensure that the command type is set
	if cc.CommandType == nil {
		return nil, fmt.Errorf("command type is not configured")
	}

	// Create a new instance of the command type (it's a pointer to a new struct)
	commandValue := reflect.New(cc.CommandType).Elem()

	// Iterate over the expected fields and populate them
	for key, expectedType := range cc.Specs() {
		value, ok := creationParams[key]
		if !ok {
			return nil, fmt.Errorf("missing required parameter: %s", key)
		}

		// Handle nested struct fields
		field := commandValue.FieldByName(key)
		if !field.IsValid() {
			return nil, fmt.Errorf("unknown field: %s", key)
		}

		// Check if it's a nested struct and set it directly
		if field.Kind() == reflect.Struct {
			if reflect.TypeOf(value) != expectedType {
				return nil, fmt.Errorf("parameter %s should be of type %s", key, expectedType)
			}
			if !field.CanSet() {
				return nil, fmt.Errorf("cannot set field: %s", key)
			}
			field.Set(reflect.ValueOf(value))
			continue
		}

		// Handle direct field assignments
		if reflect.TypeOf(value) != expectedType {
			return nil, fmt.Errorf("parameter %s should be of type %s", key, expectedType)
		}

		if !field.CanSet() {
			return nil, fmt.Errorf("cannot set field: %s", key)
		}

		field.Set(reflect.ValueOf(value))
	}

	// Convert back to an interface, assuming your command structs implement `interfaces.Command`
	command, ok := commandValue.Addr().Interface().(interfaces.Command)
	if !ok {
		return nil, fmt.Errorf("created command does not implement interfaces.Command")
	}

	return command, nil
}

// Recursive helper function to process struct fields, including nested structs
func getStructSpecs(t reflect.Type, prefix string, specs map[string]reflect.Type) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem() // Dereference pointer types
	}

	// Ensure we are working with a struct
	if t.Kind() != reflect.Struct {
		return
	}

	// Iterate over fields of the struct
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported or anonymous fields
		if !field.IsExported() {
			continue
		}

		fieldName := prefix + field.Name
		if field.Type.Kind() == reflect.Struct {
			// Treat the nested struct as a single field
			specs[fieldName] = field.Type
		} else {
			// Add field type to specs map
			specs[fieldName] = field.Type
		}
	}
}

// Specs dynamically generates the creation specs for the given command type, including nested structs
func (cc CommandCreator) Specs() map[string]reflect.Type {
	if cc.CommandType == nil {
		return nil
	}

	specs := make(map[string]reflect.Type)
	getStructSpecs(cc.CommandType, "", specs)
	return specs
}

// Name returns the command name
func (cc CommandCreator) Name() string {
	return cc.CommandType.Name()
}

func NewCommandCreator(command interface{}) CommandCreator {
	// Ensure the provided command is a struct type or a pointer to a struct
	t := reflect.TypeOf(command)
	if t.Kind() == reflect.Ptr {
		t = t.Elem() // Dereference pointer types
	}
	if t.Kind() != reflect.Struct {
		panic("command must be a struct or a pointer to a struct")
	}

	// Create and return the CommandCreator
	return CommandCreator{
		CommandType: reflect.TypeOf(command),
	}
}
