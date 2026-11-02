# Changelog

```
1.0.0   2026-11-02
        [Patch]
        * Pin the release tag to the commit that was built
        * Make the tick runner test robust to the busy-slot release window
        * Run each schedule independently and bound Paho publish writes
        * Publish without blocking, run commands in order, pick the connected Wi-Fi adapter, release tagged commits
        * Bound publish waits, run commands off the MQTT callback and queue every release run
        * Fall back to a direct start if the task relaunch fails and serialize release jobs
        * Enable shutdown privilege for sleep, keep running if relaunch fails, clear stale discovery entries
        * Fix self-update relaunch, stable boot time and install script user identity
        * Give CHANGELOG the code fence the release script expects
        * Add release workflow building Windows binaries
        * Add install script, example config and documentation
        * Add display and lock event tracking and Windows power commands
        * Add Windows audio, process, power, network and Wi-Fi sources
        * Add self-update from GitHub releases with Scheduled Task relaunch
        * Ignore retained command messages
        * Add Home Assistant discovery with lock, screen locked and IP entities
        * Add MQTT command handling for volume, mute, power and lock
        * Add sensor publishers, network rate maths and update tickers
        * Add MQTT client, publish helper and file logging
        * Add System interface, pure helpers and Linux stub
        * Add repo scaffold, design spec and config loading
        * Add design spec and implementation plan

```
