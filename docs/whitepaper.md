### – a whitepaper, draft – 
# breakthrough
## An extension for your (mostly) POSIX-compliant shell based on ncurses putting a TUI as a quasi-windows-like layer over the terminal
#### MIT, Apache or GNU-License, 2021-08-26 Version 0.0.1

#### Abstract
A pseudo-windows-like interface for (mostly) POSIX-compliant operating systems written in C/C++.

Imagine that you open a terminal session/shell to your (mostly) POSIX-compliant operating system via SSH. You expand the shell to full screen and start breakthrough on the console. After a short initialization a tidy surface of approx. 211 Columns x 59 Lines (assumed at a resolution of 1920x1080 px) appears. 

Moving the cursor to the bottom opens a simple sliding taskbar that shows the PID of  processes along with the name of applications. Mouseover displays additional information in a bubble, such as the path of the application.You can open windows that have minimize, maximize and close buttons, as well as a title bar and a search box next to it that can be used to search for regular expressions and of course offers file-globbing and a context-sensitive search suggestion (default: locate).  In addition, there are simple clickable fields for options, view settings, etc. Dropdown menus open up and radio buttons work as well as simple switches.
You can open windows that have minimize, maximize and close buttons, as well as a title bar and a search box next to it that can be used to search for regular expressions and of course offers file-globbing and a context-sensitive search suggestion (default: locate).  In addition, there are simple clickable fields for options, view settings, etc. Dropdown menus open and radio buttons function as well as simple switches.

The title bar itself shows the current path.You can select the path and copy it to the clipboard, right-click and hold makes too long paths start scrolling instead of showing an abbreviated version like "/etc/this/or/.../many/others/". Nice: if you move the cursor up or down while keeping right button pressed down you can speedup and slow down the scrolling. Useful for long paths. The segment of the path shown stays this way until you move the window or refresh it.  With double-clicking into its field OR selecting it with <TAB>-key and <ENTER> you can edit the shown part. By pressing the Enter key the scope of the window changes to the new path. The window shows the content of the current path - can be scrolled if needed. A sidebar with tree view can be shown on the left side and if needed an information window can be shown, which shows additional information like path, owner, groups and permissions (editable).The display of the folder contents can be accessed using the same options as "ls". Alternatively, one types "ls -alF" into a command field that can be opened below the window bar. Here you can also start applications that can be executed on the console ,like Midnight Commander, top/htop or iptraf, and execute them in the window. 

Windows can be touched (even with the mouse) and freely move around on the desktop. Corners follow the mouse pointer to the desired position to show the size of the window to be moved. The desktop is simple and appears empty, but should give the possibility to execute functions that are difficult or impossible to reach without a window manager. 

#### Why? 

If you think - as I do - that you would like to work in such an environment and enjoy the speed and reliability of a SSH session, the convenience of a quasi-windows-like environment and all that completely remotely if you want, then join and let's make it a reality. 

The idea is to create a modern TUI and access your (mostly) POSIX-compliant environment with ease, while benefiting from the experience of developing modern operating systems that focus on graphical user interfaces. You won't just have the best of both worlds, but of all possible worlds. Ultimately, this won't be just a window to your computer or a convenient TUI, but a revelation, an epiphany and a complete breakthrough to the best features your terminal can offer - locally or remotely. 

Want to imagine more read on, else skip to the introduction

##### an Example of a use case

Aufgaben wie das Umbennen und/oder Zusammenführen von zuvor aus unterschiedlichen Quellen markierten Dateien sollte zum Kinderspiel werden und intuitiv einfach sein.
  
Man öffnet drei Fenster. Sources A, B and Destination C. In window A you select with your mouse several files in folder ~/my_pics/ in windows B you locate all family*.jpg by using the regex-search field and if satisfied with the appearing list you select them by the newly appearing "select all listed" button. From windows A you drag&drop the selection to window C and you are ask, if you want to copy, move or bulk rename the files. you choose bulk-rename and are asked if you want add files? you want and drag&drop the selection from windows B also to windows C. asked again if you want to add you answer "no". A list of the files from window A and from several subdirectories in the selction from windows B is shown in windows C. You are asked if you want to rename the originals or copies into path of windows C. you choose originals. A dialog shows up listing options for renaminig, filters and of cours original names and names after applying your desired new name conventions. all quite similar to the renaming options of total commander. After pressing start you end up with perfect renamed pictures in several folders. you realise a picture was renamed wrongly. just use the search field in a newly opend window, but this time you click the option uttons at the right side of the input field and set it to "use find" instead of "use locate". Alternatively you have a global "ipdate-db"-button to reindex.

By the way: 
- Links to frequently used scripts or applications can be placed on the desktop or grouped in the taskbar and are available at the push of a button. 
- Files can be dragged to shortcuts to editors to be edited
- when typing, breakthorugh already suggests command completion and path selection suggestions
- tail the output of your webserver logfiles in one windows, watch your bitcoin node in another, while in a third one you see your lightning node working
- use git, docker and other cli-based services on one ssh session, but follow several open terminals on the same desktop at the same time
- Drag&drop, mouse selection, input fields, clickable & editable elements, running shells in windows – optional with tabs within the windows, minimizing windows, use of screens, regex searches, bulk rename, swichtching elements and input fields with pressing/clicking on <TAB> and switching tasks/windows with <ALT>+<TAB>, there are so many options and useful options out there to implement it seems overwhelming. 

On the other hand we're working on the console/terminal and if any is true than "**if there is a shell, there is a way!**".

At the end there is not just a great like nano, midnight commander or htop, which provides convenience and which you don't want to miss after a short time, but a comprehensive environment, in which it is fun to be not just a user, but administrator and system operator on the console - and suddenly you shall realize that you miss breakthrough when you log in via ssh and read:

```console
foo@bar:~$ breaktrough
breakthrough: command not found
```

#### Introduction 

Whitepaper is intended as a project presentation, to make the idea and the concept appealing to others, a kind of feasibility study and, of course, also as a request for feedback and criticism. Especially with regard to use, adaptation and acceptance. 

It is clear that Breakthrough is an aspirational project. The first step is to find help, because such an ambitious project cannot be carried out by one developer alone. 

breakthrough is though about to be developed under the Mozilla Public License 2.0 (MPL-2.0), which offers the possibility to trademark the name, but otherwise is fully supported by the open-source community. It would be nice, but not necessary, if one day some kind of commercial product could be made out of breakthrough. 

All kind of feedback regarding any aspect of the project is highly appreciated.

Supporters and companions wanted
breakthrough can be a great project for learning about systems programming, for teaching and sharing knowledge, or for passing on experience as an advanced developer to aspiring systems programmers. 

Should the development of the project breakthrough gain a foothold and the idea for a (mostly) POSIX-compliant TUI finds acceptance in the community, hopefully independent system programmers or those employed by large enterprises will take notice of breakthrough and take on the development. It would be a great success and achievement to bring programmers and developers of different quality together and to see them working self-motivated and with enthusiasm on a project.

#### Tools/requirements

By default, breakthrough should be basically (mostly) POSIX-compliant, but with various additional modes for more color, better display options / extended character set / more special characters / offer different color variants / color schemes.

So you should use breakthrough with as in the (mostly) POSIX-compliant default version with

```console
foo@bar:~$ breaktrough
Starting - breaking through the walls...
```
or a customized version with something like
```console
foo@bar:~$ breaktrough --color-mode 16 --char-set alternate --utf-8 --color-scheme hacker
Starting - breaking through the walls...
```
Other features and tools the project should utilize in order not to have to reinvent the wheel and to achieve a usable result as soon as possible:   
- C/C++
- Curses/libcurses (do avoid the need to invent a complete new windowing system
- as near to system libraries as possible
- Open-source (to attract ideologically motivated developers) 
- UTF-8-Support 
- as a teaching project for advanced programmers and educators for your students

#### Final sentence: 

Unfortunately, breakthrough is not even a concept at the moment, but rather just an idea for a concept. Please help already at this early stage, fork and revise this whitepaper to make it better and bring in new ideas.

Discussion on any topic related to project breakthrough on github is highly welcome. If you like the idea and approach of breakthrough, feel free to share the idea, this whitepaper and its repository to anybody you think might be interested or contribute something useful to breakthough. Thank you!
