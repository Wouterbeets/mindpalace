# MindPalace: A Local-First AI Assistant Framework

MindPalace is an open-source AI assistant engineered to prioritize user autonomy and privacy. By granting full control over data and operations, it empowers individuals to customize their AI experience without external dependencies.

## Key Features
- **Local Execution**: Runs entirely on your device using lightweight, unbiased language models, ensuring data privacy and independence from cloud services or corporate ecosystems.
- **Event-Sourced Architecture**: Built on a robust, efficient foundation that maintains a complete history of interactions for reliability and easy recovery.
- **Modular Plugin System**: Extends core functionality through agent-based plugins, such as task management or code assistance, allowing seamless integration of new capabilities.
- **Multi-Modal Input**: Supports voice and text-based interactions for intuitive configuration and control.
- **Extensibility**: Designed for adaptability, enabling users to tailor the system to their unique workflows and projects.

## Philosophy
MindPalace shifts the paradigm from prescriptive AI tools to a flexible framework where the computer adapts to the user. It provides a streamlined, professional-grade toolset for building personalized digital solutions, ideal for developers, creators, and power users seeking a trustworthy AI companion.

## Getting Started
1. **Clone the Repository**:
   ```bash
   git clone https://github.com/yourusername/mindpalace.git
   ```
2. **Install Dependencies**:
   Refer to `requirements.txt` for Python dependencies and `go.mod` for Go modules.
3. **Build and Run**:
   Use the provided `Makefile` or run directly via:
   ```bash
   go run cmd/mindpalace/main.go
   ```

## 3D Asset Pipeline
MindPalace now ships with a baked catalog of Godot-ready meshes. To rebuild or curate the library:

- Convert the Thingi10K archive into GLB assets and a manifest:
  ```bash
  go run cmd/modelbaker/main.go -tar Thingi10K-002.tar.gz
  ```
- Assets are written under `world/assets/models`, and the manifest lands in `catalog.json`. Godot consumes the `res://assets/models/<id>.glb` paths directly—no runtime extraction.
- Optional `-metadata path/to/models.json` lets you supply human-friendly titles and tags (JSON mapping `model_id` → `{ "title": "...", "tags": [...] }`) which power search during orchestration and plugin execution.
- At runtime the orchestrator loads the manifest; plugins can request models by id and capture scale hints and bounding boxes from the catalog.

## Contributing
We welcome contributions to enhance MindPalace. Please review our code of conduct, submit issues for bugs or features, and open pull requests for improvements.

## License
MindPalace is licensed under the MIT License. See [LICENSE](LICENSE) for details.

For more information, explore the [documentation](doc.txt) or contact the development team.
