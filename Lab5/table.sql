CREATE TABLE `iu9Trofimenko` (
  `id` int(11) NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `title` text COLLATE 'latin1_swedish_ci' NULL,
  `link` text COLLATE 'latin1_swedish_ci' NULL,
  `description` text COLLATE 'latin1_swedish_ci' NULL,
  `pub_date` datetime NULL
) ENGINE='InnoDB' COLLATE 'latin1_swedish_ci';
