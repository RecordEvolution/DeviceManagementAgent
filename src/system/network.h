
#include <NetworkManager.h>

void list_network_devices()
{
  NMClient        *client;
  const GPtrArray *devices;
  int             i;
  GError*         error = NULL;

  /* Get NMClient object */
  client = nm_client_new(NULL, &error);
  if (!client) {
    g_message("Error: Could not create NMClient: %s.", error->message);
    g_error_free(error);
    // return EXIT_FAILURE;
  }

  /* Get all devices managed by NetworkManager */
  devices = nm_client_get_devices(client);

  /* Go through the array and process Wi-Fi devices */
  for (i = 0; i < devices->len; i++) {
    NMDevice *device = g_ptr_array_index(devices, i);
    printf("%d: %s\n",i,nm_device_get_iface(device));
    if (NM_IS_DEVICE_WIFI(device))
      printf("WiFi device: %s\n",nm_device_get_iface(device));
    // show_wifi_device_info(device);
  }

  g_object_unref(client);
}


void list_wifi_networks()
{

}
