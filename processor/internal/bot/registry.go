package bot

// Registry maps command identifier keys to Command implementations.
type Registry struct {
	commands map[string]Command
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

// Register adds a command. Registers both Name() and all Aliases().
func (r *Registry) Register(cmd Command) {
	r.commands[cmd.Name()] = cmd
	for _, alias := range cmd.Aliases() {
		r.commands[alias] = cmd
	}
}

// Lookup returns the command for an identifier key, or nil if not found.
func (r *Registry) Lookup(key string) Command {
	return r.commands[key]
}

// All returns all unique registered commands (deduplicated by Name).
func (r *Registry) All() []Command {
	seen := make(map[string]bool)
	var result []Command
	for _, cmd := range r.commands {
		if !seen[cmd.Name()] {
			seen[cmd.Name()] = true
			result = append(result, cmd)
		}
	}
	return result
}
