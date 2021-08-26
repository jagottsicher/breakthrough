### – a whitepaper, draft – 
# breakthrough
## An extension for your POSIX-compatible shell based on ncurses putting a TUI as a quasi-windows-like layer over the terminal
#### MIT, Apache or GNU-License, 2021-08-26 Version 0.0.1

#### Abstract
A pseudo-windows-like interface for posix compatible operating systems written in C.
Alternative zu windowsmanager in graphikmodes. Man kann sich das vorstellen, wie eine version von window3.11 auf textbasis, aber mit allen features, die ein aktuelles modernes Gui mitbringt. Alle Welt will Graphik und Schnick-Schnack, aber In unix/linux-umgebungen auf dem Terminal wollen die Admins eine weniger fancy aber eine effiziente und effective Umgebung, die eine schnelle Ausführungsgeschwindigkeit hat, resourcenschonend arbeitet, sicher und reliable ist und sich flexible an den workflow des benutzers anpassen lässt.

Man stelle sich vor, dass man via SSH eine Terminalsitzung/shell zu seinem POSIX-kompatiblem Betriebsystem öffnet. Man erweitert die Shell zum Vollbild und startet breakthrough auf der console. Nach kurzer Initialisierung erscheint eine aufgeräumte Oberfläche von ca 211 Columns x 59 Lines (angenommen bei 1920 x 1080 px) . 

Mit der Maus den Cursor zum unteren Ende fahrend öffnet sich eine einfache verschiebbare Taskbar, die die PID einiger Prozesses anzeigt zusammen mit dem Namen von Anwendungen. Mouseover lässt zuätzliche Informationen in einer "Bubble" erscheinen, wie den Pfad der Anwendung. Man kann Fenster öffnen, die Minimierungs-, Maximierungs- und Schließen-button haben, ebenso wie eine Titelleiste und einem Suchfeld daneben, das sich auf eine Suche nach regulären Ausdrücken verwenden lässt und natürlich file-globbing und eine context-senstive Suchvorschläge (defauklt: locate) anbietet. Dazu gibt es einfache anklickbare Felder für Optionen, Ansichteinstellungen usw. Dropdown-Menus öffnen sich und Radiobuttons funktionen ebenso wie einfach switches.

Die Titelleiste selbst zeigt den aktuellen Pfad an. Man kann den Pfad markieren undin die Zwischenablage kopieren, richt-click and hold makes too long paths start scrolling instead of showing an abbreviated version like "/etc/this/or/.../many/others/". Nice: if you move the coursor up or down while keeping right button pressed down you can speedup and slow down the scrolling. Useful for long paths. the segment of the path shown stays this way until you move the window of refresh it. With double-clicking into its field OR selecting it with <TAB>-key and <ENTER> kann man den angezeigten Teil editieren. Mit Drücken der Entertaste wechselt der Scope des Fenster in den neuen Pfad. Das Fenster zeigt den inhalt des aktuellen Pfades – bei Bedarf scrollbar. Eine Seitenlleiste mit Baumansicht lässt sich links einblenden und bei Bedarf kann man ein Informationsfenster einblenden lassen, das zusätzliche Informationen wie Pfad, Besitzer, Gruppen und permissions (editierbar) anzeigt. Die Darstellungs der ORdnerinhalte lässt sich über die gleichen Optionen wie "ls" abrufen. Alternativ tippt man "ls -alF" in ein Befehlsfeld, das sich unterhalb der Fensterleiste öffnen lässt. Dort lassten sich auch Anwendungen, die man auch der Konsole ausführen kann wie Midnight Commander, top/htop oder iptraf, starten und im Fenter ausführen. 

Die Fenster lassen sich (auch mit der Maus) touch und freely move around on the Desktop. Der Desktop ist simple und erscheint leer, aber soll die möglichkeit geben, Funktionen auszuführen, die sich ohne Fenstermanager nur schwer oder gar nicht erreichen lassen. 

##### an Example of a use case

Aufgaben wie das Umbennen und/oder Zusammenführen von zuvor aus unterschiedlichen Quellen markierten Dateien sollte zum Kinderspiel werden und intuitiv einfach sein.
  
Man öffnet drei Fenster. Sources A, B and Destination C. In window A you select with your mouse several files in folder ~/my_pics/ in windows B you locate all family*.jpg by using the regex-search field and if satisfied with the appearing list you select them by the newly appearing "select all listed" button. From windows A you drag&drop the selection to window C and you are ask, if you want to copy, move or bulk rename the files. you choose bulk-rename and are asked if you want add files? you want and drag&drop the selection from windows B also to windows C. asked again if you want to add you answer "no". A list of the files from window A and from several subdirectories in the selction from windows B is shown in windows C. You are asked if you want to rename the originals or copies into path of windows C. you choose originals. A dialog shows up listing options for renaminig, filters and of cours original names and names after applying your desired new name conventions. all quite similar to the renaming options of total commander. After pressing start you end up with perfect renamed pictures in several folders. you realise a picture was renamed wrongly. just use the search field in a newly opend window, but this time you click the option uttons at the right side of the input field and set it to "use find" instead of "use locate". Alternatively you have a global "ipdate-db"-button to reindex.

If you – like me – think that you would like to work in such an environment and enjoy the speed and reliability of an SSH-Session, the convenience of a windows-like environment and all that completely remote if you like, then please join us and let us make that become reality. The idea is to create a modern TUI and access your POSIX-compatible Environment with easy while benefitting from the experience of the development of modern operating systems which are focussing on graphical user interfaces. You will have the best not only from two worlds, but from all possible worlds. Eventually, this will not be a windows to your computer or a convenient TUI, it will be a revelation, an epiphany and a complete breakthrough to the best features your terminal can provide – locally or remote.

#### Introduction 

  Whitepaper versteht sich zur projektcorstellung, um die idea und das konzept anderen schmackhaft zu machen, eine Art Machbarkeitsstudie und natürlich auch als aufforderung für feedback und kritik. Vor allem, was nutzen, adaption und akzeptanz angeht. 

Mitstreiter und Weggefährten gesucht
Großartiges projekt, um sich mit der systemprogrammierung vertraut zu machen oder als fortgeschrittener entwickler erfahrungen weiterzugeben and die, die für die auch raspis gedacht waren.

#### Anwendungsfall

Drag&drop, mark, input fields, clickable & editable elements, shells in windows and with tabs, etc based on ncurses! minimizing Windows, screens, Regex suche, bulk umbenennen, swichtching elements and input fields with pressing/clicking on <TAB> or switching tasks/windows with <ALT>+<TAB>, there are so many options and useful options aout there to implement it seems overwhelming. 

On the other hand we're working on the console/terminal and if any is true than "if there is a shell, there is a way!".  usw

Am Ende ein tool wie nano, midnight commander oder htop, dass Bequemlichkeit verschafft und das man nach kurzer Zeit nicht mehr missen möchte. eine Umgebung, in der es Spaß macht nicht nur ein Anwender, sondern Administrator und System operator zu sein.

#### Tools/requirements

In der Grundeinstellung POSIX-kompatibel mit verschiedenen Zusatzmodi für mehr Farbe, bessere Darstellungsoptionen / erweiterten Zeichensatz / mehr Sonderzeichen / Verschiedene Farbvarianten/Farbschemata. 

Man sollte also breakthrough mit als in der voll POSIX-kompatiblen default version mit
Verschiedene Farbvarianten. 
```console
foo@bar:~$ breaktrough
Starting - breaking through the walls...
```
oder eine angepasste Version mit 
```console
foo@bar:~$ breaktrough --color-mode 16 --char-set alternate --utf-8 --color-scheme hacker
Starting - breaking through the walls...
```
  
UTF-8
C/C++
Curses/libcurses
Systemnah
Open-source
Lernprojekt

#### Schluss: 

Momentan nur konzept, order besser gesagt nur idea für ein konzept. 

