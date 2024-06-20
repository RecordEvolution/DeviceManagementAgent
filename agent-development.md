# Agent development

## Architecture

### App, AppState and AppStore

The App structure is defined in the `common/types.go` file, as it is a widely used structure.

Each App struct also comes with a weighed semaphore `TransitionLock` property which serves as a way to 'lock' the app while it's transitioning. As well as a `StateLock` mutex property which serves as a way to prevent race conditions when the App struct is being accessed by different go routines.

### AppStore

App data is stored on seperate levels: in-memory and in the database. All CRUD operations for apps are managed by the `AppStore` struct, which is defined in `apps/app_store.go`.

The `AppStore` serves as an abstraction layer for these three storage levels. For instance, if an app is not found in memory when accessed, it will be loaded from the database, and vice versa. This abstraction simplifies the process of managing database and in-memory states manually.

Whenever an app's state is updated locally (both in-memory and in the database), it is also updated remotely in the database. This synchronization is also managed by the AppStore struct.

Apps can also be added to the `AppStore` using a `TransitionPayload` received through a Crossbar RPC. For example, whenever a new app is installed and a state transition is triggered via a Crossbar RPC, the app will be created in the `AppStore` both in-memory and in the database.

Additionally, transition payloads are loaded during the agent's boot process to populate the in-memory app storage.

### Database

#### Update scripts

The update scripts function as a one-time `.sql` file that executes each time the database is loaded at agent startup. The process does not verify if a script fails or succeeds; it simply executes each `.sql` file and disregards any errors that occur. Typically, an error will be thrown if the script has been executed previously, resulting in a no-op (no operation).

Update scripts can be added by simply creating a new `.sql` file in the `persistence/update-scripts` directory.

#### Persistence

All persistence-related functionalities are handled in the `persistence` package, with the main file being `persistence/database.go`.

The `Database` interface, located in `persistence/types.go`, serves as an abstraction for a generic database compatible with the agent's database model. The interface is designed to allow for the creation of other implementations of this database API in the future, such as a different database besides SQLite.

The implementation of the Database interface that we use throughout the app is the `AppStateDatabase`. The `AppStateDatabase` serves as an abstraction for the SQLite API that we use in order to save app states and app data on disk.

### Network

The agent allows users to manage their network settings and configurations. An abstraction for this API exists as the `Network` interface, which resides in the `network/network.go` file. Currently, there is only one implementation of this interface, which is for the NetworkManager API.

Upon agent launch, we check whether the operating system is Linux. If it is, we enable the NetworkManager API implementation; otherwise, we apply a dummy implementation that returns false values for all operations. [Reference in code](https://github.com/RecordEvolution/DeviceManagementAgent/blob/master/src/agent.go#L208)

When implementations for other operating systems are added, the dummy implementation can be replaced with a proper implementation of the `Network` interface.

### Messenger

The `Messenger` interface serves as an abstraction layer for the communication protocol used to interface with the agent externally. This interface is defined in the `messenger/types.go` file. Currently, it is implemented using WAMP (Web Application Messaging Protocol).

To implement WAMP, we use the [Nexus](https://github.com/gammazero/nexus) client.

### Container

The `Container` interface serves as an abstraction layer for any container-related operations. Currently, we have implemented this interface using the Docker SDK. This approach allows the agent to potentially support other containerization software in the future, such as Podman.

#### Compose

Since there is no official Docker Compose API, we have manually implemented and exposed an API that interacts with Docker Compose using the `docker compose` command-line tool. 

We interface with the Compose CLI using the built-in exec Go API and provide the output of each command as a string channel.


## Adding or Editing a Crossbar RPC

All Crossbar endpoints on the agent are registered in the `api/external.go` file. In said file we have a map of all registered endpoints with a topic mapped to the function that executes it.

The topics for the exposed endpoints that are registered on the agent itself are stored in the `messenger/topics/exposed.go` file.

To create a new crossbar RPC, you must first add a new `.go` file in the `api` folder with the following parameters and return value:

```
func (ex *External) exampleRPC(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	return &messenger.InvokeResult{}, nil
}
```

What is returned represents the response that is eventually sent out to the caller.

Afterwards the function must added to the map in the `api/external.go` file using the corresponding topic. The full topic (which contains the serial number is automatically added when the agent is started).

## Adding or Editing a State Machine Handler

The `stateTransitionMap` is assigned and defined in the `apps/state_machine.go` file. All states and their transition functions can be found in the `getTransitionFunc` function.

The application state constants are defined in the `common/constants.go` file. Since Go does not allow circular dependencies, elements that are reused across the entire app must be defined at a top level. New states must, therefore, be defined there.

To add a new state transition, you can create a new file in the apps folder and then create a state transition function with the following parameters and return value:

```
func (sm *StateMachine) exampleStateTransitionFunction(payload common.TransitionPayload, app *common.App, releaseBuild bool) error {
}
```

The state of the app can be changed during this transition function using `the sm.setState()` function. For example, you can change the app to an intermediate state. Whenever the transition has ended, you must manually call `sm.setState()` to determine the final state after the state transition has completed.