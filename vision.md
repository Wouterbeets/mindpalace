# MindPalace Roadtrip Vision One-Pager

## Overview
MindPalace, a local, event-sourced AI assistant, will capture a near-perfect record of a 1.5-year family roadtrip from Canada to Argentina in 2026, with a test run in the Balkans this summer. The goal is to enable homeschooling, connect with people, and break free from the 9-to-5 life, creating a once-in-a-lifetime adventure. To fund the $10,000 hardware setup, MindPalace will be monetized on Steam as a versatile tool for personal projects, with core data capture features and a plugin system for extensibility.

## Roadtrip Setup
- **Participants**: Family of six (parents and four children).
- **Vehicle**: Land Cruiser with a five-person rooftop tent for kids; Jack Bushman off-road trailer with a two-person rooftop tent for parents, featuring a fridge, kitchen, storage, and foldable sunscreen/rain cover for outdoor living.
- **Test Run**: Two-month Balkans trip this summer to validate the setup.
- **Main Trip**: 1.5-year journey from Canada to Argentina in 2026, focusing on homeschooling and cultural connections.

## MindPalace Role
MindPalace will run on a Mac Mini in the trailer, powered by dual lithium-ion batteries, using a 32B parameter LLM (e.g., Llama, Mistral) with tool-calling capabilities. It will integrate microphones, cameras, and GPS to store trip data locally, ensuring privacy. The system will enable interactive reliving of the trip (e.g., querying specific days or generating narratives) via a robust plugin architecture.

## Hardware Requirements
- **Trailer Setup**: Drone docking station, touchscreen, dual lithium-ion batteries, and solar panels.
- **Estimated Cost**: $5,300–$10,300, aligning with a $10,000 budget.
- **Drone (Personal Use)**: Custom setup using open-source tools (e.g., PX4) for $1,000–$2,000 to capture scenery and scout routes, integrated with MindPalace via a `DronePlugin`.

## Steam MVP
To fund the setup, MindPalace will be sold on Steam for $20 as a local AI assistant for projects like roadtrips. The MVP includes:
- **Core Features**:
  - **Transcription**: Real-time audio transcription for voice journals.
  - **Media Integration**: Support for photos, videos, and PDFs.
  - **Audio/Video Capture**: Seamless recording and storage.
- **Plugin System**: Extends functionality for processing data or capturing specific datasets (e.g., GPS, journals). Plugins define:
  - **Commands**: Actions the LLM can trigger (e.g., `SaveLocation`, `AddMedia`).
  - **Events**: State changes stored in the event-sourcing system (e.g., `LocationSaved`, `MediaAdded`).
  - **UI Elements**: For user interaction and feedback (e.g., Kanban boards, media galleries).
- **Plugin Generation**: Users can create plugins locally with powerful LLMs (e.g., Deepseek R1) or via a Relay server calling xAI’s Grok for enhanced plugin generation.

## Monetization Strategy
- **One-Time Purchase**: $20 for the full local MindPalace, including core features and plugin system.
- **Cloud AI Access**: Optional pay-as-you-go or monthly fee for API calls to a Relay server routing to xAI’s Grok. For every $5 in Grok tokens, users pay $7, with $2 as profit, creating recurring revenue.
- **Plugin Ownership**: Plugins created via local or cloud LLMs are owned forever, encouraging long-term tool-building for personal use.

## Personal Trailer Features
The trailer version will include a niche `DronePlugin` for:
- Launching a drone from a trailer docking station to capture scenery or scout routes.
- Automatically uploading drone media to MindPalace via the `AddMedia` command, tagged with GPS and timestamps.

## Development Timeline
- **Deadline**: Under one year until 2026 to finalize hardware (batteries, solar, drone dock) and software.
- **MVP Priorities**: Nail transcription, media, audio, and video integration for the Steam version, with plugin generation as the core extensible feature.
- **Personal Build**: Add drone integration and GPS plugins for the trailer version, leveraging MindPalace’s dynamic plugin loading.

## Vision Statement
MindPalace empowers users to capture and relive life’s adventures, from epic roadtrips to personal projects, with a local-first AI that’s extensible through user-owned plugins. For the roadtrip, it will preserve every moment in a private, interactive archive, while the Steam version democratizes this power, funding the journey and enabling others to build their own digital legacies.