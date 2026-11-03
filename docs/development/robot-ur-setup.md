# Preparar el UR10e para Excalibur

Esta guía se entrega en español por petición expresa de la tarea RB3. El
resto de los archivos de este cambio usa inglés. Las unidades del puente son
metros y radianes; una pose TCP contiene `[x,y,z,rx,ry,rz]` en la base del robot.

## 1. Antes de conectar el brazo

1. Una persona debe permanecer junto al paro de emergencia físico, con el
   espacio de trabajo despejado. Comprueba también el riesgo de atrapamiento
   de la pinza neumática, incluso con el brazo inmóvil.
2. Identifica el TCP, la carga útil y el montaje reales. No copies las
   articulaciones de prueba del simulador al brazo físico.
3. Acuerda cada movimiento en la conversación antes de que el agente lo
   ejecute. Primero pide mirar la cámara y explicar el desplazamiento, la
   velocidad y el destino. Por ejemplo: «Apruebo mover la pinza 2 cm hacia
   arriba en la base, a 0,05 m/s». Una aprobación no autoriza movimientos
   posteriores distintos.
4. Mantén visible **STOP**. El STOP del software necesita red y un puente
   operativo; usa el paro físico ante una situación peligrosa o una pérdida
   de comunicación. No dependas de la imagen de la cámara como medida de
   protección.

## 2. Configurar PolyScope 5 en el colgante

1. Abre el menú ☰ → **About / Acerca de** y anota la versión completa de
   PolyScope, el modelo y el número de serie. Esta integración espera un
   **UR10e con PolyScope 5.x**. El simulador validado usa 5.25.0; no hace falta
   actualizar el brazo para igualarlo sin revisar primero su instalación.
2. En ☰ → **Settings → System → Network**, elige dirección estática. Ejemplo
   de una subred dedicada que no debe coincidir con otras redes del portátil
   o del servidor:

   | Equipo | Dirección | Máscara |
   | --- | --- | --- |
   | UR10e | `192.168.50.10` | `255.255.255.0` |
   | Ethernet del portátil | `192.168.50.1` | `255.255.255.0` |

   Guarda los cambios. Para el cable directo no necesitas una puerta de
   enlace ni DNS en la Ethernet del portátil. Conserva su conexión a Internet
   por Wi-Fi. El enrutamiento de Tailscale usa normalmente SNAT; no añadas
   rutas hacia `100.64.0.0/10` al robot para esta configuración inicial.
3. En **Settings → Security → Services**, desbloquea con la contraseña de
   administrador y habilita **Dashboard Server**, **Primary Client
   Interface**, **Secondary Client Interface**, **Real-Time Client
   Interface** y **Real-Time Data Exchange (RTDE)**. Bloquea de nuevo el menú.
   Sus puertos son `29999`, `30001`, `30002`, `30003` y `30004`.
4. En **Settings → System → Remote Control**, habilita Remote Control y sal
   de ajustes. Después cambia de **Local** a **Remote** en el selector de la
   esquina superior derecha. Habilitar la función por sí solo no cambia el
   modo. Tras cambiarlo, vuelve a conectar Excalibur.
5. Con la instalación revisada y el área despejada, enciende los motores y
   libera los frenos. Espera `robot_mode: RUNNING` y `safety_mode: NORMAL`.
   `RUNNING` describe el estado del brazo, no implica que haya un programa
   ejecutándose (`program_running` es otro dato).

Las rutas de Remote Control y Services se apoyan en la
[preparación oficial del controlador](https://github.com/UniversalRobots/Universal_Robots_Client_Library/blob/master/doc/setup/robot_setup.rst).
La diferencia entre habilitar y seleccionar Remote está en el
[manual de PolyScope 5](https://www.universal-robots.com/manuals/EN/HTML/SW5_25/Content/prod-usr-man/software/PolyScope/content/hamburger_menu_g5/System_remote_en.htm).

## 3. Límites de seguridad y caja de trabajo

En **Installation → Safety**, una persona autorizada debe revisar los límites
de velocidad de herramienta, articulaciones, fuerza, potencia, planos y
distancias de parada para esta célula. Para la primera sesión, sugiere una
velocidad TCP de **0,10 m/s o menor** y articulaciones de **0,30 rad/s
(aproximadamente 17°/s) o menos**, sujetos a esa revisión. Configura también
los límites de **Reduced**, con valores menores que Normal y adecuados al
montaje; confirma y guarda la instalación cuando corresponda.

El puente definido para esta ronda exige `NORMAL`: si el controlador pasa a
`REDUCED`, también rechaza movimientos. No desactives una entrada o una
protección para evitar ese rechazo. Para trabajar lentamente mientras está
en `NORMAL`, reduce los límites de Normal y la configuración del puente.

En esta máquina, revisa `~/.config/excalibur/robot.json` después de integrar
RB1. Mantén `require_approval: true`; los máximos previstos por defecto son
`speed_max: 0.25` m/s y `joint_speed_max: 0.6` rad/s. Para empezar puedes
bajarlos a `0.10` y `0.30`. Define `workspace.min` y `workspace.max` con los
límites XYZ medidos **en la base**, dejando margen a la mesa y al montaje.
No copies una caja de otro robot. Los pasos relativos no deben superar
`0.15` m ni `0.5` rad; puedes reducir ambos límites.

La caja restringe destinos del TCP; por sí sola no garantiza que todo el
brazo, la pinza o la trayectoria eviten obstáculos. Comprueba las trayectorias
y conserva las protecciones de la instalación. No aumentes límites ante un
rechazo sin entender y revisar primero su causa.

## 4. Portátil Windows como router de subred Tailscale

1. Conecta la Ethernet al controlador. En la configuración IPv4 del adaptador
   pon `192.168.50.1/24`, sin puerta de enlace. Revisa el nombre del adaptador
   con `Get-NetAdapter` en PowerShell. Los ejemplos usan `Ethernet`; cámbialo
   si tu adaptador tiene otro nombre.
2. Comprueba primero la conexión local:

   ```powershell
   ping 192.168.50.10
   Test-NetConnection 192.168.50.10 -Port 29999
   Test-NetConnection 192.168.50.10 -Port 30004
   ```

3. Instala [Tailscale para Windows](https://tailscale.com/download/windows),
   inicia sesión en la misma red Tailscale que el servidor y evita que el
   portátil se suspenda durante la sesión. Activa el funcionamiento sin
   sesión de usuario si necesitas mantenerlo conectado tras cerrar sesión.
4. Abre PowerShell **como administrador**. Identifica las interfaces IPv4 y
   habilita reenvío en Ethernet y en Tailscale, usando sus nombres reales:

   ```powershell
   Get-NetIPInterface -AddressFamily IPv4 |
     Format-Table ifIndex,InterfaceAlias,ConnectionState,Forwarding
   Set-NetIPInterface -InterfaceAlias "Ethernet" -AddressFamily IPv4 -Forwarding Enabled
   Set-NetIPInterface -InterfaceAlias "Tailscale" -AddressFamily IPv4 -Forwarding Enabled
   ```

   Comprueba de nuevo `Forwarding` después de reiniciar o de reconectar un
   adaptador. Si la política de Windows revierte el ajuste, debe corregirla
   el administrador del equipo. No desactives todo el firewall: revisa las
   reglas de las interfaces implicadas. El significado del ajuste está en
   [Set-NetIPInterface de Microsoft](https://learn.microsoft.com/en-us/powershell/module/nettcpip/set-netipinterface).
5. Anuncia **la dirección de red**, no la dirección individual del brazo:

   ```powershell
   tailscale up --advertise-routes=192.168.50.0/24
   ```

   Si Tailscale ya está configurado y pide repetir otras opciones de `up`,
   conserva esas opciones o cambia solo este ajuste:

   ```powershell
   tailscale set --advertise-routes=192.168.50.0/24
   ```

6. En la consola de administración de Tailscale, abre el portátil en
   **Machines**, busca sus rutas de subred y aprueba `192.168.50.0/24`.
   Comprueba además que la política de acceso permite al servidor llegar
   a esa subred y a los puertos del robot. Anunciar, aprobar y permitir
   tráfico son pasos distintos. Conserva el SNAT predeterminado.

El procedimiento de anuncio y aprobación se basa en la
[guía oficial de routers de subred para Windows](https://tailscale.com/docs/features/subnet-routers?tab=windows).

## 5. Comprobar la ruta desde esta máquina Linux

Con Tailscale instalado y conectado a la misma red:

```sh
sudo tailscale up --accept-routes
tailscale status
ping -c 3 192.168.50.10
nc -zv -w 3 192.168.50.10 29999 30004
nc -zv -w 3 192.168.50.10 30001 30002 30003
```

Si `up` pide otras opciones previas, consérvalas o usa
`sudo tailscale set --accept-routes=true`. El portátil debe seguir encendido,
con Ethernet y Tailscale conectados. Un ping bloqueado no demuestra por sí
solo que los puertos TCP estén inaccesibles.

Después de integrar RB1, ejecuta desde la raíz del repositorio:

```sh
extensions/robot-ur/setup.sh
```

El contrato prevé que el daemon inicie el puente al conectar desde la página
**Robot**. Su Python será `~/.local/share/excalibur/robot-venv/bin/python`,
o el indicado en `EXCALIBUR_ROBOT_PYTHON`. Conecta a `192.168.50.10`, no a
la IP Tailscale del portátil. El navegador puede abrir la interfaz por el
túnel público; la conexión TCP al robot sale del daemon Linux por Tailscale.
El puerto HTTP del puente `7811` permanece local al servidor.

**Estado de entrega RB3:** esta rama solo aporta el simulador, las pruebas
y esta guía. El puente, su script de instalación, la herramienta `robot` y
la página Robot requieren integrar los cambios de RB1/RB2. Si no aparecen,
no interpretes su ausencia como un problema del brazo o de Tailscale.

## 6. Colocar y compartir la cámara

1. Fija el portátil o teléfono fuera del alcance del brazo. Deben verse la
   pinza, la mesa y los posibles obstáculos, con luz suficiente. Evita reflejos,
   vibración y oclusiones por el propio brazo.
2. Abre Excalibur mediante **HTTPS** en el dispositivo que aporta la cámara;
   permite el acceso a ella y, tras integrar RB2, compártela desde Robot.
   Abrir la página en el portátil no comparte automáticamente la cámara del
   teléfono: abre la sesión también en el teléfono para usar su cámara.
3. Verifica la imagen y la fecha de actualización antes de cada movimiento.
   Si está congelada, detén la operación y vuelve a compartirla. Mantén la
   pestaña activa; algunos móviles suspenden la cámara al bloquearse.
4. Marca físicamente los ejes de la base y aclara qué significa «izquierda».
   La izquierda de la imagen no equivale necesariamente a `+X` o `+Y` del
   robot. Una imagen monocular no proporciona una medida fiable de profundidad.

## 7. Enseñar `home`, `mesa` y `recoger`

Estas poses son propias del montaje. `home` debe ser una postura despejada;
`mesa`, una aproximación segura por encima de la mesa; `recoger`, el punto de
recogida con orientación y altura verificadas. Comprueba antes la pinza:
**DO1** cierra por defecto con `True` y abre con `False`, pero hay que verificar
el cableado y `gripper_close_value`; **DI1/DI2** son sensores y su presencia
no está simulada como una pinza física en URSim.

1. Vacía el área, sujeta la herramienta si es necesario y prepara la parada
   física. Para enseñar desde el colgante, cambia a **Local** y usa su botón
   de freedrive. Remote deshabilita el freedrive del colgante.
2. Lleva lentamente el brazo a la postura deseada. Suelta freedrive y
   comprueba que quedó inmóvil. Vuelve a **Remote**, reconecta el puente si
   hace falta y pide al agente: «Guarda la pose actual como home, sin mover
   el brazo». La acción del contrato es `save_pose`, que registra el TCP actual.
3. Repite para `mesa` y `recoger`, comprobando altura, orientación y ausencia
   de obstáculos. Pide `list_poses` y verifica los tres nombres. El puente
   guardará las poses en `~/.config/excalibur/robot-poses.json` del servidor.
4. Si enseñas mediante freedrive remoto del puente, pide explícitamente su
   activación al agente, mantén supervisión física, después pide desactivarlo
   y confirma ese estado antes de guardar. No alternes órdenes automáticas
   con manipulación manual. Si no se desactiva, usa STOP y el control local.
5. Solicita una prueba a velocidad baja y aprueba cada tramo. Una pose
   guardada no implica que el camino hasta ella esté libre. La acción `home`
   del agente usa la pose llamada `home`; no implica una calibración del robot.

## 8. Resolver problemas sin reintentar movimientos a ciegas

| Síntoma | Comprobación y siguiente paso |
| --- | --- |
| Protective stop | STOP y revisión presencial de contacto, carga, TCP, límites y obstáculos. Corrige la causa antes de reconocer el paro desde el colgante. No automatices un bucle de desbloqueo. Después confirma NORMAL/RUNNING y aprueba de nuevo el movimiento. |
| `not in remote control` | Comprueba tanto la habilitación en Settings → System como la selección Remote en la cabecera. Regresa de Local a Remote y reconecta. El simulador puede aceptar comandos aun indicando Local; el brazo real exige Remote. |
| RTDE refused / timeout | Desde Windows prueba `30004`; después desde Linux. Habilita RTDE y Primary/Secondary en Services, revisa los demás puertos y cierra otro cliente de control que esté usando los mismos registros RTDE. Conserva el error del puente. En URSim 5.25.0 usa el probe con `ur_rtde==1.6.5` y 500 Hz descrito abajo. |
| Ruta de subred inaccesible | Primero comprueba Windows → brazo. Después `tailscale ping <nombre-del-portatil>` desde Linux, anuncio/aprobación de la ruta, política de acceso, `--accept-routes`, reenvío IPv4 en ambas interfaces, firewall y ausencia de subredes solapadas. Un túnel web accesible no prueba la ruta al robot. |
| Destino fuera de caja / aprobación ausente | Revisa unidades, marco base/tool, caja y paso relativo. Pide un destino dentro de límites y una nueva aprobación clara; no desactives la comprobación. |
| Estado REDUCED, IDLE o POWER_OFF | El puente debe rechazar movimiento. Comprueba presencialmente la causa y la instalación antes de restaurar NORMAL; libera frenos cuando proceda y espera RUNNING. |
| Cámara ausente o antigua | Revisa HTTPS, permisos del navegador, dispositivo seleccionado, pestaña activa y sello de tiempo. No muevas hasta tener una imagen actual y supervisión. |

## 9. Ensayar hoy con URSim, sin tocar el brazo real

Desde la raíz del repositorio:

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

Solo se publican puertos en `127.0.0.1`; abre el colgante simulado en
`http://127.0.0.1:6080/vnc.html?autoconnect=true`. Para verlo desde otro
equipo, usa un túnel SSH local a `6080`; no publiques los puertos del robot.
La prueba verifica el contenedor local y no acepta la IP del brazo real.
Consulta [el README de URSim](../../extensions/robot-ur/ursim/README.md)
para Remote/Services y los comandos Dashboard, y
[el de e2e](../../extensions/robot-ur/e2e/README.md) para iniciar el puente
y distinguir pruebas ejecutadas de pruebas omitidas.

La frecuencia de 500 Hz evita un problema del simulador descrito en las
[notas oficiales de PolyScope 5.25](https://www.universal-robots.com/articles/ur/release-notes/release-note-software-version-525x/).
Este ensayo comprueba comunicaciones, telemetría y movimientos simulados;
no valida las protecciones, la neumática ni el recorrido del brazo físico.
