import argparse
import os

import novaclient.client as nova_client
from keystoneauth1 import identity, session


def openstack_check_env_vars() -> bool:
    """Return true if there are OpenStack credentials in environment variables.

    The openstack session requires the following environment variables:
    - OS_AUTH_URL
    - OS_APPLICATION_CREDENTIAL_ID
    - OS_APPLICATION_CREDENTIAL_SECRET
    If any of these are missing, this function will return False. Therefore,
    this function should be called before calling create_openstack_session.
    """
    for env_var in [
        "OS_AUTH_URL",
        "OS_APPLICATION_CREDENTIAL_ID",
        "OS_APPLICATION_CREDENTIAL_SECRET",
    ]:
        if env_var not in os.environ:
            return False
    return True


def create_openstack_session() -> session.Session:
    """Return a session object for the OpenStack cloud.

    This function leverages the OpenStack keystone package to create
    a connection to the OpenStack cloud using the application credentials
    stored in the environment variables.
    """
    os_auth_url = os.getenv("OS_AUTH_URL")
    os_application_credential_id = os.getenv("OS_APPLICATION_CREDENTIAL_ID")
    os_application_credential_secret = os.getenv("OS_APPLICATION_CREDENTIAL_SECRET")

    auth = identity.v3.application_credential.ApplicationCredential(
        auth_url=os_auth_url,
        application_credential_id=os_application_credential_id,
        application_credential_secret=os_application_credential_secret,
    )
    return session.Session(auth=auth)


def main(instance_id: str) -> object:
    # Set the mitmproxy environment variables
    os.environ["https_proxy"] = "https://127.0.0.1:8080"
    home_directory = os.path.expanduser("~")
    mitmproxy_ca_cert = f"{home_directory}/.mitmproxy/mitmproxy-ca-cert.pem"
    os.environ["REQUESTS_CA_BUNDLE"] = mitmproxy_ca_cert

    # Create OpenStack session
    has_env_vars = openstack_check_env_vars()
    if not has_env_vars:
        print("[!] Missing credentials... Exiting.")
        exit()
    sess = create_openstack_session()
    nova_c = nova_client.Client(2.83, session=sess)

    instance = nova_c.servers.get(instance_id)
    print(f"{instance_id}: {instance.status}")

    security_groups = instance.list_security_group()
    for sg in security_groups:
        print(f"Security Group: {sg.name}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("instance_id", help="The instance ID to fetch")
    args = parser.parse_args()
    instance_id = args.instance_id
    main(instance_id)
