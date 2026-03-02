import json
import logging
import os
from pathlib import Path
from typing import Optional
from urllib.parse import urlparse
from uuid import UUID

from mitmproxy import ctx, http
from mitmproxy.log import ALERT

endpoints = {
    # https://dashboard.rc.nectar.org.au/project/api_access/
    # openstack service list -c Name -c Type
    "accelerator": "https://accelerator.rc.nectar.org.au/",
    "account": "https://accounts.rc.nectar.org.au/api/",
    "alarming": "https://alarming.rc.nectar.org.au/",
    "allocations": "https://allocations.rc.nectar.org.au/rest_api/",
    "application-catalog": "https://application-catalog.rc.nectar.org.au/",
    "cloudformation": "https://cloudformation.rc.nectar.org.au/",
    "compute": "https://compute.rc.nectar.org.au/",
    "container-infra": "https://container-infra.rc.nectar.org.au/",
    "database": "https://database.rc.nectar.org.au/",
    "dns": "https://dns.rc.nectar.org.au/",
    "identity": "https://identity.rc.nectar.org.au/",
    "image": "https://image.rc.nectar.org.au/",
    "key-manager": "https://key-manager.rc.nectar.org.au/",
    "load-balancer": "https://load-balancer.rc.nectar.org.au/",
    "message": "https://message.rc.nectar.org.au/",
    "metric": "https://metric.rc.nectar.org.au/",
    "nectar-ops": "https://status.rc.nectar.org.au/api/",
    "nectar-reservation": "https://nectar-reservation.rc.nectar.org.au/",
    "network": "https://network.rc.nectar.org.au/",
    "object-store": "https://object-store.rc.nectar.org.au/",
    "orchestration": "https://orchestration.rc.nectar.org.au/",
    "outage": "https://status.rc.nectar.org.au/api/",
    "placement": "https://placement.rc.nectar.org.au/",
    "rating": "https://rating.rc.nectar.org.au/",
    "reservation": "https://reservation.rc.nectar.org.au/",
    "s3": "https://swift.rc.nectar.org.au/",
    "security": "https://security.rc.nectar.org.au/",
    "share": "https://share.rc.nectar.org.au/",
    "sharev2": "https://share.rc.nectar.org.au/",
    "volumev3": "https://volume.rc.nectar.org.au/",
}


class NARC:
    """NARC - Nectar Access Rules Creator."""
    def __init__(self):
        """Run first, before load."""
        # Addon options
        self.output_filename = "access_rules"
        self.wildcard_uuid_in_path = True
        self.wildcard_suffix_in_path = True

        # Addon data structures
        self.access_rules = list()

        # Load configuration options
        self.endpoints = endpoints

    def load(self, loader):
        """Run when addon is loaded, adds optional arguments."""
        loader.add_option(
            name="output",
            typespec=Optional[str],
            default="access_rules",
            help="Specify a custom output file name",
        )
        loader.add_option(
            name="uuid",
            typespec=Optional[bool],
            default=True,
            help="Wildcard UUIDs from API path",
        )
        loader.add_option(
            name="wildcard",
            typespec=Optional[bool],
            default=True,
            help="Use wildcard on base API paths (less secure, more simple)."
        )

    def configure(self, updated):
        """Run when configuration is updated."""
        if "output" in updated:
            self.output_filename = ctx.options.output
        if "uuid" in updated:
            self.wildcard_uuid_in_path = ctx.options.uuid
        if "wildcard" in updated:
            self.wildcard_suffix_in_path = ctx.options.wildcard

    def request(self, flow: http.HTTPFlow) -> None:
        """Handle all HTTP/S requests from mitmdump."""
        url = flow.request.url
        method = flow.request.method

        # Skip anything that is not a request to the nectar.org.au domain
        hostname = urlparse(url).hostname or ""
        if not hostname.endswith("nectar.org.au"):
            return

        logging.log(ALERT, ">>> Narcing on HTTP request...")

        # Check the request URL matches a known Nectar API endpoint
        matching_service_name = next(
            (name for name, path in self.endpoints.items() if url.startswith(path)),
            None,
        )
        if matching_service_name is None:
            logging.log(ALERT, f">>> No matching endpoint for URL: {url}")
            return

        tmp_access_rule = {
            "service": matching_service_name,
            "method": method,
            "path": None,  # Not yet processed
            "url": url,
        }

        # Log the result to file (sanitize URL to prevent log injection)
        safe_url = url.replace("\n", "\\n").replace("\r", "\\r")
        log_string = f"{method} {safe_url}\n"
        log_opener = lambda p, flags: os.open(p, flags, 0o600)  # noqa: E731
        with open(f"{Path(__file__).name}.log", "a", opener=log_opener) as f:
            f.write(log_string)

        self.access_rules.append(tmp_access_rule)

    def done(self):
        """Run when exiting mitmproxy."""
        logging.log(ALERT, ">>> Processing output...")
        processed_access_rules = list()

        # Loop all findings and preprocess before export
        for access_rule in self.access_rules:
            path = access_rule.get("url")

            # Log the path to stdout
            logging.log(ALERT, f">>> Raw path: {path}")

            # Remove endpoint prefix from path (HTTP, domain, port)
            path = path.replace(endpoints[access_rule["service"]], "")

            # Remove parameters from URL
            path = path.split("?")[0]

            # Replace UUID with * in path segment, if requested (on by default)
            if self.wildcard_uuid_in_path:
                path_fixed = list()
                path_segments = path.split("/")
                for path_segment in path_segments:
                    try:
                        _ = UUID(path_segment)
                        path_fixed.append("**")
                    except ValueError:
                        path_fixed.append(path_segment)

                path = ("/").join(path_fixed)

            # Add wildcard to path suffix, if requested (on by default)
            if self.wildcard_suffix_in_path:
                if path.endswith("/"):
                    path = f"{path}**"

            # Prepend slash to path
            path = f"/{path}"

            logging.log(ALERT, f">>> Fix path: {path}")

            processed_access_rule = {
                "service": access_rule["service"],
                "method": access_rule["method"],
                "path": path,
            }
            processed_access_rules.append(processed_access_rule)

        # Create a dict of unique keys (method + | + path)
        unique_access_rules = {
            f"{access_rule['method']}|{access_rule['path']}": access_rule for access_rule in processed_access_rules
        }
        # Remove the keys to get a list of values
        unique_access_rules = list(unique_access_rules.values())

        # Save output to a file (owner read/write only)
        json_opener = lambda p, flags: os.open(p, flags, 0o600)  # noqa: E731
        with open(f"{self.output_filename}.json", "w", opener=json_opener) as f:
            json.dump(unique_access_rules, f, indent=4)


addons = [NARC()]
