# TODO - Roadmap

## Implemented

- [x] Volume level and mute (monitor and control)
- [x] Active application
- [x] Display on/off (event driven)
- [x] Screen locked (event driven) and lock command
- [x] User active / idle time with configurable threshold
- [x] Network upload/download rates
- [x] IP address
- [x] Last boot time
- [x] Battery (when present)
- [x] Wi-Fi SSID and signal strength (when an adapter is present)
- [x] Sleep, display sleep, shutdown, reboot commands
- [x] Home Assistant MQTT auto-discovery with BIOS manufacturer/model
- [x] Debug and dry-run modes
- [x] Auto-update from GitHub releases with Scheduled Task relaunch
- [x] Per-user Scheduled Task installer

## Planned

### Security & connectivity
- [ ] TLS for the broker connection (`mqtt_tls`, CA and client certificate options)
- [ ] Custom topic prefix

### Sensors
- [ ] Resolve the real app behind `ApplicationFrameHost` for Store apps
- [ ] Window title of the foreground window
- [ ] CPU and memory usage
- [ ] Disk usage
- [ ] Temperatures and fan speeds via LibreHardwareMonitor
- [ ] Power source (AC / battery) and charging state
- [ ] Currently playing media (Windows media session)

### Commands
- [ ] Hibernate
- [ ] Brightness control
- [ ] Media controls (play/pause, next, previous)
- [ ] Per-application volume
- [ ] Launch / quit application

### Runtime
- [ ] System tray icon with connection status
- [ ] Optional service mode for the sensors that do not need a desktop
- [ ] Configurable update intervals and selective metric publishing
