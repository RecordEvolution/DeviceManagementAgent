#![allow(dead_code)]
#![allow(unused_variables)]

use bollard::container::{Config, CreateContainerOptions, LogsOptions, StartContainerOptions};
use bollard::image::BuildImageOptions;
use bollard::models::BuildInfo;
use bollard::Docker;
use futures_util::stream::StreamExt;
use futures_util::stream::TryStreamExt;
use oldTokio::runtime::Runtime;
use std::collections::HashMap;
use std::error::Error;
use std::fs::File;
use std::future::Future;
use std::io::Read;
use std::pin::Pin;
use tokio::sync::mpsc::UnboundedReceiver;
use wamp_async::{Client, ClientConfig, WampError};

#[macro_use]
extern crate lazy_static;

const TEST_IMAGE: &'static str = "rust_container_test:latest";
const TEST_CONTAINER: &'static str = "rust_container_test";
const WS_URI: &'static str = "ws://localhost:8080/ws";

async fn build() {
    let mut build_image_labels = HashMap::new();
    build_image_labels.insert("maintainer", "somemaintainer");

    let build_image_options = BuildImageOptions {
        dockerfile: "Dockerfile",
        t: TEST_IMAGE,
        q: false,
        nocache: false,
        cachefrom: vec![],
        pull: true,
        rm: true,
        forcerm: true,
        networkmode: "host",
        platform: "linux/x86_64",
        ..Default::default()
    };

    let mut file = File::open("TestApp.tar").unwrap();
    let mut contents = Vec::new();
    file.read_to_end(&mut contents).unwrap();

    DOCKER_SOCKET
        .build_image(build_image_options, None, Some(contents.into()))
        .map(|v| {
            println!("{:?}", v);
            v
        })
        .map_err(|e| {
            println!("{:?}", e);
            e
        })
        .collect::<Vec<Result<BuildInfo, bollard::errors::Error>>>()
        .await;
}

async fn run() {
    DOCKER_SOCKET
        .create_container(
            Some(CreateContainerOptions {
                name: TEST_CONTAINER,
            }),
            Config {
                image: Some(TEST_IMAGE),
                ..Default::default()
            },
        )
        .await
        .unwrap();

    DOCKER_SOCKET
        .start_container(TEST_CONTAINER, None::<StartContainerOptions<String>>)
        .await
        .unwrap();
}

async fn logs() {
    let options = Some(LogsOptions::<String> {
        stdout: true,
        follow: true,
        timestamps: true,
        ..Default::default()
    });

    let mut stream = DOCKER_SOCKET.logs(TEST_CONTAINER, options);
    while let Some(item) = stream.next().await {
        println!("{:?}", item);
    }
}

// async fn init_session() {}

async fn create_connection(
    uri: &str,
) -> (
    Client<'static>,
    Pin<Box<dyn Future<Output = std::result::Result<(), WampError>> + Send>>,
    Option<UnboundedReceiver<Pin<Box<dyn Future<Output = Result<(), WampError>> + Send>>>>,
) {
    let (client, (evt_loop, _rpc_evt_queue)) = Client::connect(
        uri,
        Some(ClientConfig::default().set_ssl_verify(false)),
    )
    .await
    .unwrap();

    (client, evt_loop, _rpc_evt_queue)
}

async fn setup_crossbar(realm: &str) -> Client<'static> {
    println!("Attempting to connect");

    let (mut client, evt_loop, _rpc_evt_queue) = create_connection(WS_URI).await;
    // Connect to the server
    println!("Connected to {:?}", WS_URI);

    // Spawn the event loop
    tokio::spawn(evt_loop);

    println!("Joining realm {:?}", realm);
    let _ = client.join_realm(realm).await.unwrap();

    client
}

async fn register_subscriptions(client: &Client<'_>) {
    let (build_id, mut build_queue) = client.subscribe("reswarm.containers.build").await.unwrap();
    println!("Subscribed to reswarm.containers.build");

    tokio::spawn(async move {
        let mut rt = Runtime::new().unwrap();

        rt.block_on(async {
            loop {
                match build_queue.recv().await {
                    Some((_pub_id, args, kwargs)) => {
                        build().await;
                    }
                    None => println!("Event queue closed"),
                };
            }
        })
    });

    let (run_id, mut run_queue) = client.subscribe("reswarm.containers.run").await.unwrap();
    println!("Subscribed to reswarm.containers.run");

    tokio::spawn(async move {
        let mut rt = Runtime::new().unwrap();

        rt.block_on(async {
            loop {
                match run_queue.recv().await {
                    Some((_pub_id, args, kwargs)) => {
                        run().await;
                    }
                    None => println!("Event queue closed"),
                };
            }
        })
    });

    let (helloworld_id, mut helloworld_queue) = client
        .subscribe("reswarm.containers.helloworld")
        .await
        .unwrap();

    println!("Subscribed to reswarm.containers.helloworld");

    tokio::spawn(async move {
        loop {
            match helloworld_queue.recv().await {
                Some((_pub_id, args, kwargs)) => {
                    println!("Event(args: {:?}, kwargs: {:?})", args, kwargs)
                }
                None => println!("Event queue closed"),
            };
        }
    });
}

lazy_static! {
    #[cfg(unix)]
    static ref DOCKER_SOCKET: Docker = Docker::connect_with_unix_defaults().unwrap();
}

#[tokio::main()]
async fn main() -> Result<(), Box<dyn Error>> {
    let client = setup_crossbar("realm1").await;

    register_subscriptions(&client).await;

    tokio::time::sleep(std::time::Duration::from_secs(10 * 60)).await;

    Ok(())
}
