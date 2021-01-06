// ------------------------------------------------------------------------- //

#include <iostream>
#include "wampcc/wampcc.h"


int main(int argc, char* argv[])
{
  std::cout<<"starting (C++)-WAMP client...\n";

  /* Create the wampcc kernel, configured to support SSL. */
  wampcc::config conf;
  conf.ssl.enable = true;
  wampcc::kernel the_kernel(conf, wampcc::logger::console());

  /* Create the TCP socket and attempt to connect. */
  std::unique_ptr<wampcc::tcp_socket> socket(new wampcc::tcp_socket(&the_kernel));
  socket->connect("127.0.0.1", 55555).wait_for(std::chrono::seconds(3));

  /* Create the SSL socket, in connector mode. */
  // wampcc::ssl_socket socket(&the_kernel);

  // try to connect
  // socket.connect("cb.reswarm.io",8080).wait_for(std::chrono::seconds(3));
  // socket->connect("127.0.0.1", 55555).wait_for(std::chrono::seconds(3));

  if (!socket->is_connected())
    throw std::runtime_error("connect failed");

  /* With the connected socket, create a wamp session & logon to the realm
   * called 'default_realm'. */
  auto session = wampcc::wamp_session::create<wampcc::websocket_protocol>(&the_kernel,
                                                                          std::move(socket));

  session->hello("default_realm").wait_for(std::chrono::seconds(3));

  if (!session->is_open())
    throw std::runtime_error("realm logon failed");

  /* Call a remote procedure. */
  session->call("greeting", {}, {},
    [](wampcc::wamp_session&, wampcc::result_info result) {
      if (result)
        std::cout << "got result: " << result.args.args_list[0] << std::endl;
      }
  );

  // session->call("math.service.add", {}, {{100, 200}, {}},
  //                   [](wampcc::wamp_session&, wampcc::result_info result) {
  //       if (result)
  //         std::cout << "got result: " << result.args.args_list[0] << std::endl;
  //     });

  /* Register a procedure that can sum an array of numbers. */
  session->provide("math.service.add", {},
    [](wampcc::wamp_session&, wampcc::registered_info info) {
      if (info)
        std::cout << "procedure registered with id "
                  << info.registration_id << std::endl;
      else
        std::cout << "procedure registration failed, error "
                  << info.error_uri << std::endl;
    },
    [](wampcc::wamp_session& ws, wampcc::invocation_info info) {
      int total = 0;
      for (auto& item : info.args.args_list)
        if (item.is_int())
          total += item.as_int();
      ws.yield(info.request_id, {total});
    }
  );

  session->closed_future().wait_for(std::chrono::seconds(10));
  session->close().wait();

  std::cout<<"finishing (C++)-WAMP client...\n";

  return 0;
}

// ------------------------------------------------------------------------- //
