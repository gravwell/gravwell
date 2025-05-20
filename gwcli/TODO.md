- create CSV exclude variant
- create JSON exclude variant
    - currently blocked by an issue in the [Gabs library](https://github.com/Jeffail/gabs). I have a PR open to fix it, but Gabs may be unmaintained, which would suck because it is cool as hell.
- create Table exclude variant

^ all three of these are pretty low priority, especially given you can just call StructFields and feed the resulting []string to the normal To* subroutines.

- the `--all` flag in various list commands is not really respected as admin mode is not implemented

- implement no-color flag
    - I have it stuck in my brain that lipgloss has native support for the [NoColor environment variable](https://no-color.org/), but it does not seem to currently be effectual.
    - This can probably be rememdied by providing lipgloss with the NoColor or 1bit renderer, but you would need to ensure all lipgloss styles use this renderer, likely as a new singleton tightly coupled with stylesheet.

- tree/query/actor.go's BurnFirstView...
    - There is a substantial exploration of the issue in BurnFirstView()

- search around for a BubbleTea rendering fix for window size messages in non-altmode.
    - currently, get artefacting above most recent draw
    - this may be beyond the capabilties of Bubble Tea (without always running in alt buffer mode or claiming the entire terminal on boot some other way), as so many terminal applications artefact badly on resize (at least for no-longer-controlled elements).

- BUG: DS's results lose the alternating color if the start of the entry is cut off (aka: the termainl escape characters get cut off at the start)

- support X-Y notation in datascope records downloading

- support RecordsPerPage flag/option in datascope for larger/higher-density displays
    - currently pinned at 25, which can leave a lot of empty space if the terminal is full-screened
    - could also have it adjust page count and records per page dynamically with WindowSizeMessages
    - This will likely be altered/considered when properly paginating the download (as part of Milestone 7)

- add debouncer to DS to reduce lag when holding a key
    - native debouncer bubble, though I do not have any experience using it

- utilize DataScope's table's native filtering
    - provide keybind and external ("API") filter TI
        - the external API is best as the table has no idea where the viewport is, so the filter would need to be a part of the viewport to move with it.
    - will require utilizing the table's update method, which is currently not called
        - somewhat conflicts with the viewport wrapper
        - remember to disable the table's keybinds, other than filtering

- support more FieldTypes (radio buttons, checkboxes) in scaffold create

- `extractor create`: figure out how to support dynamic module suggestion based on current tags (as the web GUI does)
    - `ExploreGenerate()` returns a map where the keys are extraction modules, but it appears to be a costly operation to then only use the keys (module names).
        - There must be a better way to filter the list of module names.
    - Suggestions would need to automatically update whenever a new, valid tag is punched into the tags TI
        - this TI must be aware of the other TI, meaning the function signature of this customTI feature likely needs references to other parts of the createModel.
            - might be more trouble than it is worth, as we are trying to operate off the generic scaffoldcreate.

- Differentiate gravwell client library between unrecoverables and invalids
    - A number of code snippets in gwcli differentiate between "invalid parameters" and "an unrecoverable error". The former is displayed to the user, the latter is logged and gwcli gracefully returns to Mother. The client library does not make this differentiation, through no fault of its own; this is just due to different design philosophies. Therefore, all errors returned by the Client library are treated as unrecoverable. For example, scaffoldedit has implementors return `invalid` or `err` in the update function they supply. Macro edit's update function, `Client.UpdateMacro()` returns an error. These are always returned as `err`, even though many are validation errors (such as "name cannot have spaces"). More granular differentiation would be really nice (and also more consistent) from a user perspective. Implementing these changes mean either changing the Client library, performing pre-checks for known validation issues (as Macro edit's update function does for the spaces issue), or digging into the errors returned to check for known validation errors. The first one isn't reasonable because the client library shouldn't have to care about this use case. The second one isn't ideal because it duplicates validation. Finally, the third one is arguably the least desirable because it violates the principles of opaque error handling([1](https://dave.cheney.net/2016/04/27/dont-just-check-errors-handle-them-gracefully), [2](https://dave.cheney.net/2016/04/07/constant-errors)) and is also likely to get very slow very quickly ([3](https://www.dolthub.com/blog/2024-05-31-benchmarking-go-error-handling/)).

- Performance profiling and optimization
    - gwcli was developed on two fairly high performance machines and the focus was on completeness, rather than optimization. A coarse performance pass, especially in the realm of startup, would likely be beneficial for lower end machines.
    - Another area to examine: Datascope's table views can take quite a while to spin up, especially with significant amounts of data. Mother handles this reasonably gracefully, but it still isn't ideal. The bottleneck here is likely generating the underlying table and a viewport to contain and scroll it. Paginating the table would do wonders for improving speed.

- Expand full list text
    - The default list delegate truncates (with an ellipsis) list item titles and descriptions beyond the width of the list. There is no way to view the full text of the item.
    - Option 1: wrap the text of the selected item (ex: `delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Width(m.Width)`) so it can be fully displayed when highlighted. The issue here is that it will displace the list and may require dynamically altering its height as to not write over other, lower items.
    - Option 2: provide a keybind to display the full item description in a popup.

- Clean up the confirmation dialog used by delete in interactive mode
    - I slapped together the confirm mode very quickly, without much time spent on .View(). It could use at least some colorization and centering

- Built in commands (quit, history) are not displayed with context help

- BUG: race condition displaying history line (pushToHistory) sometimes causes the history line to be printed after the result of a basic (/other very fast) action
    - Bubble Tea messages do not guarentee order unless you use `.Sequence()` and this only guarentees that the `.Sequence()`ed Cmds will be ordered. Currently, pushToHistory is sent immediately in order to display the previous command before any output from it (so it looks like a normal shell). However, this is technically a race condition. Actions that are *very* fast (ex: some basics) can sometimes tea.Print their results immediately after pushToHistory is sent. Because order is not guarenteed, Bubble Tea then has a chance to process the later tea.Println first, causing results to be displayed on top of the history display that invoked them.