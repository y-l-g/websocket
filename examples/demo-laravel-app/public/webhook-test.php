<?php
$input = file_get_contents('php://input');
file_put_contents('webhook.log', $input . PHP_EOL, FILE_APPEND);