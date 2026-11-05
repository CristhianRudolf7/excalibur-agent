# Set up a UR10e for Excalibur

The integration targets UR10e controllers running PolyScope 5.x. Bridge units
are meters and radians; a TCP pose is `[x,y,z,rx,ry,rz]` in the robot base frame.
The bridge, agent tool and Robot page exist in this tree. Simulator evidence
is not verification of a physical installation.

## Before connecting

Keep a trained operator beside the physical emergency stop, with the work area
clear. Review the actual TCP, payload, mounting and pneumatic gripper hazards.
Never transfer simulator joint positions to a physical arm.

Approve each proposed movement in conversation after reviewing its direction,
speed and destination. For example: “Move the gripper up 2 cm in the base frame
at 0.05 m/s.” Approval of one movement does not authorize later movements.
Keep the software STOP visible, but use the physical emergency stop for danger
or lost communication. Software STOP depends on a working network and bridge;
camera images are not a protective measure.

## Controller configuration

Record the model and full PolyScope version in **About**. URSim verification
used 5.25.0; that does not require upgrading a physical controller without
reviewing its installation.

Assign a static address on a dedicated, non-overlapping robot network. Use
`<robot-ip>`, `<router-ip>` and `<robot-subnet>` below as placeholders for your
own installation. No private installation addresses are prescribed here.

Enable the required controller services in **Settings → Security → Services**:
Dashboard Server, Primary Client, Secondary Client, Real-Time Client and RTDE
(ports 29999, 30001, 30002, 30003 and 30004). Restore the settings lock afterward.
Follow the vendor's [controller setup guide](https://github.com/UniversalRobots/Universal_Robots_Client_Library/blob/master/doc/setup/robot_setup.rst).

Enable Remote Control under **Settings → System → Remote Control**, then select
**Remote** in the header profile menu. Enabling the setting alone does not
switch modes. Reconnect Excalibur after switching. The
[PolyScope Remote Control manual](https://www.universal-robots.com/manuals/EN/HTML/SW5_25/Content/prod-usr-man/software/PolyScope/content/hamburger_menu_g5/System_remote_en.htm)
describes both steps and the restrictions on pendant operations.

After an authorized on-site review, power the motors and release the brakes.
Wait for `robot_mode: RUNNING`; this does not mean a program is running.
`program_running` is a separate field.

## Limits and workspace

An authorized person must review the controller's tool/joint speed, force,
power, safety planes and stopping limits for the actual cell. Set Normal and
Reduced limits appropriate to that installation; no generic speed is a safety
certification.

The bridge configuration is `robot.json` in the platform config directory
(`~/.config/excalibur` on Linux and macOS). Keep `require_approval: true`.
The current defaults in `bridge/safety.py` are `speed_max: 0.25` m/s and
`joint_speed_max: 0.6` rad/s. Lower them as required by the installation review.
Define `workspace.min` and `workspace.max` from measured base-frame XYZ limits,
with margins for the table and mounting. Relative moves are bounded by the
bridge; review its current settings before use.

The current bridge accepts `NORMAL` and `REDUCED` controller safety states;
other safety states reject motion. Do not bypass a protective input to clear
an error. The TCP box checks destinations, not the whole arm, gripper or path.
A destination inside it does not prove that a trajectory avoids obstacles.

## Optional remote network connection

Direct daemon-to-controller connectivity is sufficient. If using a Windows
laptop as a Tailscale subnet router, connect its Ethernet interface to the
robot network, give it the selected router address and keep the laptop awake.
Use actual interface names and addresses in these PowerShell examples:

```powershell
$RobotAddress = '<robot-ip>'
Test-NetConnection $RobotAddress -Port 29999
Test-NetConnection $RobotAddress -Port 30004
Get-NetIPInterface -AddressFamily IPv4 |
  Format-Table ifIndex,InterfaceAlias,ConnectionState,Forwarding
```

The network administrator must configure forwarding on the relevant adapters,
advertise the robot subnet, approve its route, and permit access to the robot
ports. These are separate steps. Preserve existing Tailscale settings and
firewall protections. See the current [Tailscale subnet router guide](https://tailscale.com/docs/features/subnet-routers?tab=windows)
for platform-specific requirements; this is an optional topology, not an
Excalibur dependency.

From a Linux daemon host on that network, check TCP reachability using the real
address:

```sh
nc -zv -w 3 <robot-ip> 29999 30004
nc -zv -w 3 <robot-ip> 30001 30002 30003
```

A blocked ping alone does not prove that TCP is unavailable. Connect Excalibur
to the controller address, not the router's VPN address. A working web tunnel
does not establish a route from the daemon to the robot. Never publish robot
controller ports or the bridge HTTP port to the Internet.

## Start the bridge and share a camera

On Linux or macOS, from the repository root:

```sh
extensions/robot-ur/setup.sh
```

The daemon starts the bridge when connecting from **Robot**. The default Python
is `~/.local/share/excalibur/robot-venv/bin/python` on Linux/macOS, or the
platform data directory's `robot-venv/Scripts/python.exe` on Windows.
`EXCALIBUR_ROBOT_PYTHON` overrides it. The setup script is a shell script;
a native Windows Python environment must be provisioned separately. Bridge
HTTP defaults to localhost port 7811.

Mount a camera outside the arm's reach so the gripper, table and obstacles are
visible. Open Excalibur over HTTPS on the camera device and share it from the
Robot page. Opening the app on another computer does not share the phone's
camera. Verify the timestamp before movement; a stale image requires stopping
and restoring the camera. Keep the tab active. Image-left is not necessarily
robot `+X` or `+Y`, and a monocular view cannot reliably measure depth.

## Teach named poses

Use names appropriate to your installation, such as `home`, `table` and
`pickup`. A saved pose does not establish a safe route to it. Verify the
actual gripper wiring and `gripper_close_value`; the default DO1 close value is
`true`, open is `false`. DI1/DI2 are sensors; URSim does not reproduce physical
pneumatics.

1. Clear the area and prepare the physical stop. For pendant teaching, switch
   to Local and use its freedrive control. Remote disables pendant freedrive.
2. Move slowly to the desired pose, release freedrive and verify the arm is
   stationary. Return to Remote, reconnect if necessary, and ask the agent to
   save the current pose without moving. `save_pose` records the current TCP.
3. Repeat for other poses, then check `list_poses`. The bridge persists them in
   `robot-poses.json` in its configuration directory.
4. Remote freedrive requires an explicit request and physical supervision.
   Request its deactivation and confirm it before saving or automatic motion.
   If it does not stop, use STOP and local control.
5. Test only reviewed paths with fresh approval for each movement. `home` uses
   the saved pose named `home`; it is not a calibration operation.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Protective stop | Stop and inspect contact, payload, TCP, limits and obstacles on site; correct the cause before acknowledging at the pendant. Never automate an unlock loop. |
| Not in remote control | Check both the enabled setting and selected Remote profile; reconnect. Simulator acceptance does not establish physical-arm behavior. |
| RTDE refused or timed out | Check port 30004 from each network hop, required services and competing RTDE clients. Retain the bridge error. |
| Subnet unavailable | Check local robot access, router connection, advertised/approved routes, access policy, forwarding, firewall and overlapping subnets. |
| Target or approval rejected | Check units, reference frame, workspace and relative step. Request a valid target and fresh approval; preserve the check. |
| IDLE or POWER_OFF | Inspect the physical installation before powering on or releasing brakes. |
| Stale camera | Restore HTTPS, permissions, device selection, active tab and current timestamps before proceeding. |

## Rehearse with URSim

From the repository root:

```sh
extensions/robot-ur/ursim/ursim.sh up
python3 extensions/robot-ur/ursim/dashboard.py status
python3 extensions/robot-ur/ursim/dashboard.py ready
python3 -m venv extensions/robot-ur/e2e/.venv
extensions/robot-ur/e2e/.venv/bin/python -m pip install -r extensions/robot-ur/e2e/requirements.txt
extensions/robot-ur/e2e/run.sh direct
EXCALIBUR_URSIM_TESTS=1 extensions/robot-ur/e2e/.venv/bin/python -m pytest extensions/robot-ur/e2e -m ursim -v -rs
extensions/robot-ur/ursim/ursim.sh down
extensions/robot-ur/ursim/ursim.sh status
```

The simulator publishes ports only on localhost. Its pendant is at
`http://127.0.0.1:6080/vnc.html?autoconnect=true`; use a local SSH tunnel for
remote viewing. The tests require the local simulator and reject a physical
robot address. See [URSim](../../extensions/robot-ur/ursim/README.md) and
[e2e verification](../../extensions/robot-ur/e2e/README.md) for dependencies,
bridge startup, the pinned RTDE probe and skipped-test reporting. This checks
simulated communication and movement, not physical protections or trajectories.
